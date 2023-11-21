package controller

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/smtp"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/vishvananda/netlink"
	"go.bug.st/serial"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"h.a.agent/utils"
)

const LIMIT = 5

var RetryDB int
var RetrySerial int
var RetryICMP int
var RetryDBLcl int
var Count int

var FLAG_NETWORK *bool  //Flag untuk network check dari instance master
var FLAG_DB_LOCAL *bool //Flag untuk db local check dari instance master

var FLAG_ICMP *bool        //Flag untuk icmp ping dari instance standby
var FLAG_SERIAL *bool      //Flag untuk serial ping dari instance standby
var FLAG_PORT_REMOTE *bool //Flag untuk db ping dari instance standby

var DB_LOCAL string
var REP_STAT bool

var FLAG_SHUTDOWN *bool
var FLAG_PING_ERR *bool

var TRUE = true
var FALSE = false
var randString = map[int]string{0: "Hello ping", 1: "Hello world", 2: "This is ping", 3: "Knock knock", 4: "Ping is coming"}

var L *utils.Log = utils.L

type Controller struct {
	Timeout    time.Duration
	connStrDB  string
	HostRemote string
	PortRemote string
	serialName string
	gateway    string
	email      string
	mailServer string
	vip        string
}

func NewController(timeout time.Duration, connstr string, rmthost string, rmtport string, serialname string, gateway string, email string, mailServer string, vip string) *Controller {

	return &Controller{timeout, connstr, rmthost, rmtport, serialname, gateway, email, mailServer, vip}
}

func (c *Controller) Run() {

	//FLAG_ICMP = &TRUE
	var wg sync.WaitGroup
	var mu sync.Mutex

	rand.Seed(time.Now().UnixNano())

	wg.Add(1)
	go c.dbType("postgres", &wg)
	time.Sleep(1 * time.Second)
	if DB_LOCAL == "master" {
		c.setVIP()
		wg.Add(4)
		go c.dbCheck("postgres", &wg)
		go c.netCheck(&wg)
		go c.serialMaster(c.serialName, &wg, &mu)
		go c.waitFlagMaster(&wg)
	} else {
		wg.Add(5)
		go c.icmpPing(&wg)
		go c.dbPing(&wg)
		go c.serialStandby(c.serialName, &wg, &mu)
		go c.dbCheck("postgres", &wg)
		go c.waitFlagStandby(&wg)
	}
	wg.Wait()

	L.INF("Exiting controller...")
}

func (c *Controller) waitFlagMaster(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		if FLAG_DB_LOCAL == nil && FLAG_NETWORK == nil {
			//Jika semua flag masih belum bernilai
			L.INF("Waiting for flags to be ready.....")
		} else if FLAG_DB_LOCAL != nil && FLAG_NETWORK != nil {
			if !*FLAG_DB_LOCAL || !*FLAG_NETWORK {
				L.INF("Connection error, shutting down device.....")
				wg.Done()
				break
			} else {
				L.INF("Connection stable, continue monitoring.....")
			}
		}

		time.Sleep(10 * time.Second)
	}
}

// waitFlag melakukan print status flag
func (c *Controller) waitFlagStandby(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		if FLAG_ICMP == nil && FLAG_SERIAL == nil && FLAG_PORT_REMOTE == nil {
			//Jika semua flag masih belum bernilai
			L.INF("Waiting for flags to be ready.....")
		} else if FLAG_ICMP != nil && FLAG_SERIAL != nil && FLAG_PORT_REMOTE != nil {
			//Jika semua flag telah bernilai

			if *FLAG_PORT_REMOTE && *FLAG_SERIAL && *FLAG_ICMP {
				//jika port remote ok dan serialping ok
				L.DBG("FLAG_ICMP: %v | FLAG_DB: %v | FLAG_SERIAL: %v", *FLAG_ICMP, *FLAG_DB_LOCAL, *FLAG_SERIAL)
				L.INF("Connection is stable, continue monitoring.....")
			} else if !*FLAG_PORT_REMOTE && !*FLAG_SERIAL && !*FLAG_ICMP {
				//jika port remote false dan serialping false
				L.INF("Connection error from master, issuing promote.....")
				if *FLAG_DB_LOCAL {
					//JIka db lokal ok, baru promosi
					err := c.promoteStandby()
					if err != nil {
						L.ERR(err, "Error promoting, something wrong")
					} else {
						L.INF("Successfully promoting standby")
						c.setVIP()
						FLAG_PING_ERR = &TRUE
						alertToEmail("ALERT", "Successfully promoting standby db", c.email, c.mailServer)
					}
					break
				} else {
					//Jika db lokal error, promosi tidak dapat dilakukan
					L.INF("DB local is error, cannot promote.....")
				}
				break
			} else {
				L.INF("Something is wrong!!")
				if !*FLAG_PORT_REMOTE {
					L.DBG("FLAG_PORT_REMOTE : %v", *FLAG_PORT_REMOTE)
				}
				if !*FLAG_SERIAL {
					L.DBG("FLAG_SERIAL : %v", *FLAG_SERIAL)
				}
				if !*FLAG_ICMP {
					L.DBG("FLAG_ICMP : %v", *FLAG_ICMP)
				}
			}

		} else {
			//Memberitahu nilai flag yang sudah ada
			L.INF("Waiting for results.....")
			if FLAG_ICMP != nil && !*FLAG_ICMP {
				L.DBG("FLAG_ICMP : %v", *FLAG_ICMP)
			}
			if FLAG_SERIAL != nil && !*FLAG_SERIAL {
				L.DBG("FLAG_SERIAL : %v", *FLAG_SERIAL)
			}
			if FLAG_PORT_REMOTE != nil && !*FLAG_PORT_REMOTE {
				L.DBG("FLAG_PORT_REMOTE : %v", *FLAG_PORT_REMOTE)
			}
		}
		time.Sleep(10 * time.Second)
	}
}

func (c *Controller) icmpPing(wg *sync.WaitGroup) {
	defer wg.Done()

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		L.ERR(err, "Error creating ICMP connection")
		return
	}
	defer conn.Close()

	destAddr, err := net.ResolveIPAddr("ip4", c.HostRemote)
	if err != nil {
		L.ERR(err, "Error resolving IP address")
		return
	}

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("This is icmp ping"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		L.ERR(err, "Error marshaling ICMP message")
		return
	}

	var wgX sync.WaitGroup
	flag.Parse()

	for RetryICMP <= LIMIT {

		wgX.Add(2)
		go func() {
			defer wgX.Done()
			_, er := conn.WriteTo(msgBytes, destAddr)
			if er != nil {
				L.ERR(er, "Error sending ICMP packet")
			}
		}()

		go func() {
			defer wgX.Done()
			reply := make([]byte, 1500)

			conn.SetReadDeadline(time.Now().Add(c.Timeout))
			_, _, err := conn.ReadFrom(reply)
			if err != nil {
				L.ERR(err, "Error receiving reply")
				RetryICMP++
				if RetrySerial <= LIMIT {
					L.INF("Retrying icmp ping")
				} else {
					FLAG_ICMP = &FALSE
					L.INF("Limit retry reached, error in icmp ping")
				}
				// msg, err := icmp.ParseMessage(ipv4.ICMPTypeEcho.Protocol(), reply[:n])
				// if err == nil {
				// 	if msg.Type == ipv4.ICMPTypeEchoReply {
				// 		echoReplyData := msg.Body.(*icmp.Echo).Data
				// 		rtt := time.Since(startTime)
				// 		fmt.Printf("Received ICMP Echo Reply from %s (RTT=%v): Data=%s\n", peer, rtt, string(echoReplyData))
				// 		ReplyReceived <- true
				// 	}
				// } else {
				// 	fmt.Println("Error parsing ICMP message: ", err)
				// }
			} else {
				//fmt.Println(currentTime() + "Reply received")
				FLAG_ICMP = &TRUE
				RetryICMP = 0
				time.Sleep(1 * time.Millisecond)
			}
		}()

		wgX.Wait()

	}

}

// portCheck digunakan oleh standby untuk mengecek bila port yang akan digunakan remote db apakah menerima koneksi atau tidak
func (c *Controller) dbPing(wg *sync.WaitGroup) {
	defer wg.Done()

	for RetryDB <= LIMIT {
		conn, err := net.DialTimeout("tcp", c.HostRemote+":"+c.PortRemote, c.Timeout)
		if err != nil {
			L.ERR(err, "Error dialing")
			RetryDB++
			if RetryDB <= LIMIT {
				L.INF("Retrying to ping db")
				time.Sleep(1 * time.Second)
			} else {
				FLAG_PORT_REMOTE = &FALSE
			}
		} else {
			conn.Close()
			FLAG_PORT_REMOTE = &TRUE
			//fmt.Println(currentTime()+"Successfully connected to ", c.HostRemote+":"+c.PortRemote)
		}

		time.Sleep(20 * time.Millisecond)
	}
	L.DBG("Stopping ping db.... returning flag dbping: %v", *FLAG_PORT_REMOTE)

}

// serialStandby mengirimkan sebuah req/write ke master dan menunggu ack dari master, bila tidak ada maka akan diulangi hingga jumlah tertentu
func (c *Controller) serialStandby(serialName string, wg *sync.WaitGroup, mu *sync.Mutex) {
	defer wg.Done()

	config := &serial.Mode{
		BaudRate: 9600,
	}

	port, err := serial.Open(serialName, config)
	if err != nil {
		//fmt.Println("SerialPingStandby|Error connecting serial port: ", err)
		panic(err)
	} else {
		L.DBG("Connect success %s", serialName)
	}

	defer port.Close()
	//fmt.Printf("port: %v\n", port)

	buffer := make([]byte, 100)
	var n int
	for RetrySerial <= LIMIT {
		//Alur loop di standby : kirim req - nunggu ack - terima ack - kirim req.....

		dataToSend := []byte(randString[rand.Intn(5)])
		//fmt.Println("standby write")
		//Standby mengirimkan request/ping ke master
		_, err = port.Write(dataToSend)
		if err != nil {
			L.ERR(err, "ErrorSerial sending data")
			RetrySerial++
			if RetrySerial <= LIMIT {
				L.DBG("Trying to reconnect......")
				L.DBG("Attempting retry %v.....", RetrySerial)
			}
		} else {
			//fmt.Println("standby baca")

			//Standby akan menunggu respons ack dari master dengan timeout 5 detik
			n, err = readWithTimeout(port, buffer, c.Timeout)

			if err == context.DeadlineExceeded {
				//Jika terjadi timeout, maka akan mengirim ulang ping
				L.DBG("TimeoutSerial reached while waiting for response......")
				RetrySerial++
				if RetrySerial <= LIMIT {
					L.DBG("Trying to resend ping....")
					L.DBG("Attempting retry %v....", RetrySerial)
				}
			} else {
				//Jika ada respon dari master
				resp := string(buffer[:n])
				if err != nil || !strings.Contains(resp, "ack") {
					L.ERR(err, "ErrorSerial receiving data")
					RetrySerial++
					if RetrySerial <= LIMIT {
						L.DBG("Trying to resend the data......")
						L.DBG("Attempting retry %v.....", RetrySerial)
					}
				} else {
					//Respons ack berhasil diterima, memberikan true
					// fmt.Println("-------------------------------------------")
					// fmt.Printf(currentTime()+DB_LOCAL+"|DataSerial received: %s\n", string(buffer[:n]))
					RetrySerial = 0
					mu.Lock()
					FLAG_SERIAL = &TRUE
					// fmt.Println(currentTime() + DB_LOCAL + "|Serial ping success: " + fmt.Sprint(*FLAG_SERIAL))
					// fmt.Println("-------------------------------------------")
					mu.Unlock()
				}
			}

		}
		time.Sleep(1 * time.Second)

	}
	//Standby akan memberikan false jika telah retry lebih dari 5 kali dan masih error
	FLAG_SERIAL = &FALSE
	L.DBG("Stopping Serial ping, returning flag serial: %v", *FLAG_SERIAL)
}

func readWithTimeout(port serial.Port, buffer []byte, timeout time.Duration) (int, error) {
	done := make(chan struct{})

	var n int
	var err error

	go func() {
		n, err = port.Read(buffer)
		close(done)
	}()

	select {
	case <-done:
		return n, err
	case <-time.After(timeout):
		return 0, context.DeadlineExceeded
	}

}

// serialMaster untuk menunggu write/req dari standby dan mengirimkan ack kepada standby
func (c *Controller) serialMaster(serialName string, wg *sync.WaitGroup, mu *sync.Mutex) {
	defer wg.Done()

	config := &serial.Mode{
		BaudRate: 9600,
	}

	port, err := serial.Open(serialName, config)
	if err != nil {
		L.ERR(err, "Error connecting serial port")
	}
	defer port.Close()
	//fmt.Printf("port: %v\n", port)

	// var n int
	buffer := make([]byte, 100)
	for (RetrySerial <= LIMIT) && (FLAG_SHUTDOWN == nil) {
		//Alur loop di master : nunggu req - terima req - kirim ack - nunggu req.....

		// fmt.Println("master baca")
		//Master akan selalu menunggu request dari standby
		_, err = port.Read(buffer)
		if err != nil {
			L.ERR(err, "ErrorSerial receiving data")
			RetrySerial++
			if RetrySerial <= LIMIT {
				L.DBG("Waiting data....")
				L.DBG("Attempting retry %v.....", RetrySerial)
			}
		} else {

			//Master berhasil mendapatkan request dari standby
			// fmt.Println("-------------------------------------------")
			// fmt.Printf(currentTime()+DB_LOCAL+"|DataSerial received: %s\n", string(buffer[:n]))
			// fmt.Println("-------------------------------------------")

			dataToSend := []byte("ack")
			// fmt.Println("master write")
			//Master mengirimkan respon ack kepada standby sebagai tanda bahwa request telah diterima
			_, err = port.Write(dataToSend)
			if err != nil {
				L.ERR(err, "ErrorSerial sending data")
				RetrySerial++
				if RetrySerial <= LIMIT {
					L.DBG("Resend data....")
					L.DBG("Attempting retry %v.....", RetrySerial)
				}
			} else {
				RetrySerial = 0
			}

		}

		//Kembali ke awal, menunggu request
	}
	//Master akan memberikan false jika dilakukan retry lebih dari 5 kali dan masih error
	FLAG_SERIAL = &FALSE
	L.DBG("Stopping ping Serial, returning flag serial: %v", *FLAG_SERIAL)
}

// dbType mengecek jenis db lokal apakah replica atau master
func (c *Controller) dbType(dbDriver string, wg *sync.WaitGroup) {
	defer wg.Done()

	conn, err := sql.Open(dbDriver, c.connStrDB)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	queryRep := "SELECT usename FROM pg_stat_replication"

	queryType := "SELECT pg_is_in_recovery()"

	var res string

	for (FLAG_SHUTDOWN == nil) || (FLAG_PING_ERR == nil) {
		err = conn.QueryRow(queryType).Scan(&res)

		//Check if it is master or replica
		if err != nil {
			L.ERR(err, "Error executing query")
			break
		} else {
			if res == "true" {
				DB_LOCAL = "replica"
			} else if res == "false" {

				// Check if the replication is working in local db master
				err = conn.QueryRow(queryRep).Scan(&res)

				if err != nil {
					if err == sql.ErrNoRows {
						L.ERR(err, "Something is wrong with the replication")
						alertToEmail("Alert", "Replication is down", c.email, c.mailServer)
						REP_STAT = false
						break
					} else {
						L.ERR(err, "Error executing query: ")
					}
					DB_LOCAL = "undefined"
				} else {
					DB_LOCAL = "master"
					REP_STAT = true
				}
			}

			L.DBG("This database is a %s, replication stat: %v", DB_LOCAL, REP_STAT)
			time.Sleep(10 * time.Second)
		}
	}

}

// dbCheck melakukan pengecekan db lokal untuk mengetahui apakah up atau tidak
func (c *Controller) dbCheck(driverDB string, wg *sync.WaitGroup) {
	defer wg.Done()

	for (RetryDBLcl <= 1) && ((FLAG_SHUTDOWN == nil) && (FLAG_PING_ERR == nil)) {
		conn, err := sql.Open(driverDB, c.connStrDB)
		if err != nil {
			L.ERR(err, "Error connecting to database")
			RetryDBLcl++
			if RetryDBLcl <= 1 {
				L.DBG("Trying to restart postgres service")
				restartPostgres()
			} else {
				FLAG_DB_LOCAL = &FALSE
				FLAG_SHUTDOWN = &TRUE
			}
		} else {
			var res int

			query := "SELECT 1"

			err = conn.QueryRow(query).Scan(&res)
			// if Count > 9 {
			// 	res = -1
			// }
			if err != nil || res != 1 {
				L.ERR(err, "Error querying to database")
				RetryDBLcl++
				if RetryDBLcl <= 1 {
					L.DBG("Trying to restart postgres service")
					restartPostgres()
				} else {
					FLAG_DB_LOCAL = &FALSE
					FLAG_SHUTDOWN = &TRUE
				}
			} else {
				FLAG_DB_LOCAL = &TRUE
				RetryDBLcl = 0
				//fmt.Println(currentTime() + "dbCheck " + fmt.Sprint(*FLAG_DB_LOCAL))
			}
		}
		conn.Close()

		//Interval cek selama 10 milidetik
		time.Sleep(10 * time.Millisecond)
		// Count++

	}
	FLAG_DB_LOCAL = &FALSE
	L.DBG("Stopping check localdb.... returning flag db: %v", *FLAG_DB_LOCAL)
}

func (c *Controller) netCheck(wg *sync.WaitGroup) {
	defer wg.Done()

	gateway, err := getDefaultGateway()
	if err != nil {
		L.ERR(err, "Error")
	}

	L.DBG("Default gateway: %s", gateway)

	// ipAddr, err := net.ResolveIPAddr("ip4", gateway)
	// if err != nil {
	// 	fmt.Println(currentTime()+"Error: ", err)
	// }

	// fmt.Println("Resolved IP Address: ", ipAddr)

	for FLAG_SHUTDOWN == nil {
		conn, err := net.DialTimeout("ip4:icmp", gateway, c.Timeout)
		if err != nil {
			L.ERR(err, "Error connect to gateway")
			FLAG_NETWORK = &FALSE
			FLAG_SHUTDOWN = &TRUE
			break
		} else {
			defer conn.Close()
			//fmt.Println(currentTime() + "Success connect to gateway")
			FLAG_NETWORK = &TRUE
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func restartPostgres() {

	cmd := exec.Command("sudo", "systemctl", "restart", "postgresql")

	err := cmd.Run()
	if err != nil {
		L.ERR(err, "~~Error restarting postgresql~~")
		return

	}

	L.DBG("~~Successfully restarting postgresql~~")

}

func (c *Controller) promoteStandby() error {
	conn, err := sql.Open("postgres", c.connStrDB)
	if err != nil {
		L.ERR(err, "Error connecting to db for promote")
		return err
	}

	if DB_LOCAL == "replica" {
		query := "SELECT pg_promote()"
		_, err := conn.Exec(query)
		if err != nil {
			L.ERR(err, "Error executing query for promote")
			return err
		}
	}
	return nil
}

func (c *Controller) setVIP() {
	netmask := 24
	ifaceName := getInterfaceNetworkbyIP(c.vip)
	if ifaceName == "" {
		var err error
		L.ERR(err, "No related segment IP of VIP")
		return
	}

	cidr := fmt.Sprintf("%s/%d", c.vip, netmask)
	ipNet, err := netlink.ParseIPNet(cidr)
	if err != nil {
		L.ERR(err, "Error parsing ipNet")
		return
	}

	link, er := netlink.LinkByName(ifaceName)
	if er != nil {
		L.ERR(er, "Error getting network interface")
		return
	}

	addr := &netlink.Addr{IPNet: ipNet, Label: ""}
	err = netlink.AddrAdd(link, addr)
	if err != nil {
		L.ERR(err, "Error adding virtual IP address")
		return
	}

	L.DBG("Virtual IP %s was added to interface %s", c.vip, ifaceName)
}

func getDefaultGateway() (string, error) {
	var gateway string

	switch runtime.GOOS {
	case "darwin":
		gateway = "/sbin/netstat -nr | /usr/bin/grep default | /usr/bin/awk '{print $2}'"
	case "linux":
		gateway = "/sbin/ip route | /usr/bin/awk '/default/ { print $3 }'"
	default:
		return "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	cmd := exec.Command("sh", "-c", gateway)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), err
}

func alertToEmail(subject string, body string, emailTarget string, mailServer string) {
	// mail := gomail.NewMessage()
	// mail.SetHeader("From", "suhailie2401@gmail.com")
	// mail.SetHeader("To", emailTarget)
	// mail.SetHeader("Subject", subject)
	// mail.SetBody("text/plain", body)

	// d := gomail.NewDialer("smtp.gmail.com", 587, "suhailie2401@gmail.com", "alxi25@-")
	// d.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	// if err := d.DialAndSend(mail); err != nil {
	// 	L.ERR(err, "Error sending alert")
	// 	panic(err)
	// }

	from := "suhailie20@gmail.com"
	password := "isorslwhaskfuodj "
	to := emailTarget

	msg := "Subject: " + subject + "\r\n" + body

	auth := smtp.PlainAuth("", from, password, mailServer)

	err := smtp.SendMail("smtp.gmail.com:587", auth, from, []string{to}, []byte(msg))
	if err != nil {
		L.ERR(err, "Error sending alert")
	}
}

func getInterfaceNetworkbyIP(vip string) string {
	interfaces, err := net.Interfaces()
	if err != nil {
		L.ERR(err, "Failed to list network interfaces")
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			L.ERR(err, "Failed to get addresses of interfaces")
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipNet.Contains(net.ParseIP(vip)) {
				return iface.Name
			}
		}
	}

	return ""
}

func unixSocket() {
	socket, err := net.Listen("unix", "/tmp/echo.sock")
	if err != nil {
		L.ERR(err, "Failed to create unix socket")
		return
	}

	for {
		conn, err := socket.Accept()
		if err != nil {
			L.ERR(err, "Failed to accept incoming connection")
			return
		}

		go func(conn net.Conn) {
			defer conn.Close()

			buff := make([]byte, 1024)

			n, err := conn.Read(buff)
			if err != nil {
				L.ERR(err, "Error reading incoming data")
				return
			}

			_, err = conn.Write(buff[:n])
			if err != nil {
				L.ERR(err, "Error writing data")
				return
			}
		}(conn)
	}
}
