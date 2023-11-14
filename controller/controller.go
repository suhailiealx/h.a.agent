package controller

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"math/rand"
	"net"
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
)

const LIMIT = 5

var RetryDB int
var RetrySerial int
var RetryICMP int
var RetryDBLcl int
var Count int

var FLAG_NETWORK *bool
var FLAG_DB_LOCAL *bool
var FLAG_SERIAL *bool
var FLAG_PORT_REMOTE *bool
var FLAG_ICMP *bool
var DB_LOCAL string
var REP_STAT bool
var FLAG_SHUTDOWN *bool
var FLAG_PING_ERR *bool

var TRUE = true
var FALSE = false
var randString = map[int]string{0: "Hello ping", 1: "Hello world", 2: "This is ping", 3: "Knock knock", 4: "Ping is coming"}

type Controller struct {
	Timeout    time.Duration
	connStrDB  string
	HostRemote string
	PortRemote string
	serialName string
	vip        string
}

func NewController(timeout time.Duration, connstr string, rmthost string, rmtport string, serialname string, vip string) *Controller {

	return &Controller{timeout, connstr, rmthost, rmtport, serialname, vip}
}

func (c *Controller) Run() {

	//FLAG_ICMP = &TRUE
	var wg sync.WaitGroup
	var mu sync.Mutex

	rand.Seed(time.Now().UnixNano())

	// connMaster := "user=postgres password=postgres dbname=recordings host=localhost port=5434 sslmode=disable"
	// connStandby := "user=postgres password=postgres dbname=recordings host=localhost port=5433 sslmode=disable"

	c.dbType("postgres", &wg)
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

	fmt.Println("Exiting controller...")
}

func (c *Controller) waitFlagMaster(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		if FLAG_DB_LOCAL == nil && FLAG_NETWORK == nil {
			//Jika semua flag masih belum bernilai
			fmt.Println(currentTime() + "Waiting for flags to be ready.....")
		} else if FLAG_DB_LOCAL != nil && FLAG_NETWORK != nil {
			if !*FLAG_DB_LOCAL || !*FLAG_NETWORK {
				fmt.Println(currentTime() + "Connection error, shutting down device.....")
				wg.Done()
				break
			} else {
				fmt.Println(currentTime() + "Connection stable, continue monitoring.....")
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
			fmt.Println(currentTime() + "Waiting for flags to be ready.....")
		} else if FLAG_ICMP != nil && FLAG_SERIAL != nil && FLAG_PORT_REMOTE != nil {
			//Jika semua flag telah bernilai

			if *FLAG_PORT_REMOTE && *FLAG_SERIAL && *FLAG_ICMP {
				//jika port remote ok dan serialping ok
				fmt.Printf(currentTime()+"FLAG_ICMP: %v | FLAG_DB: %v | FLAG_SERIAL: %v \n", *&FLAG_ICMP, *FLAG_DB_LOCAL, *FLAG_SERIAL)
				fmt.Println(currentTime() + "Connection is stable, continue monitoring.....")
			} else if !*FLAG_PORT_REMOTE && !*FLAG_SERIAL && !*FLAG_ICMP {
				//jika port remote false dan serialping false
				fmt.Println(currentTime() + "Connection error from master, issuing promote.....")
				if *FLAG_DB_LOCAL {
					//JIka db lokal ok, baru promosi
					err := c.promoteStandby()
					if err != nil {
						fmt.Println("Error promoting, something wrong: ", err)
					} else {
						fmt.Println("Successfully promoting standby")
						c.setVIP()
						FLAG_PING_ERR = &TRUE
						break
					}
				} else {
					//Jika db lokal error, promosi tidak dapat dilakukan
					fmt.Println("DB local is error, cannot promote.....")
				}
				break
			} else {
				fmt.Println(currentTime() + "Something is wrong!!")
				if !*FLAG_PORT_REMOTE {
					fmt.Printf(currentTime()+"FLAG_PORT_REMOTE : %v\n", *FLAG_PORT_REMOTE)
				}
				if !*FLAG_SERIAL {
					fmt.Printf(currentTime()+"FLAG_SERIAL : %v\n", *FLAG_SERIAL)
				}
				if !*FLAG_ICMP {
					fmt.Println(currentTime()+"FLAG_ICMP : %v\n", *FLAG_ICMP)
				}
			}

		} else {
			//Memberitahu nilai flag yang sudah ada
			fmt.Println(currentTime() + "Waiting for results.....")
			if FLAG_ICMP != nil && !*FLAG_ICMP {
				fmt.Printf(currentTime()+"FLAG_ICMP : %v\n", *FLAG_ICMP)
			}
			if FLAG_SERIAL != nil && !*FLAG_SERIAL {
				fmt.Printf(currentTime()+"FLAG_SERIAL : %v\n", *FLAG_SERIAL)
			}
			if FLAG_PORT_REMOTE != nil && !*FLAG_PORT_REMOTE {
				fmt.Printf(currentTime()+"FLAG_PORT_REMOTE : %v\n", *FLAG_PORT_REMOTE)
			}
		}
		time.Sleep(10 * time.Second)
	}
}

func (c *Controller) icmpPing(wg *sync.WaitGroup) {
	defer wg.Done()

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		fmt.Println(currentTime()+"Error creating ICMP connection: ", err)
		return
	}
	defer conn.Close()

	destAddr, err := net.ResolveIPAddr("ip4", c.HostRemote)
	if err != nil {
		fmt.Println(currentTime()+"Error resolving IP address: ", err)
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
		fmt.Println(currentTime()+"Error marshaling ICMP message: ", err)
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
				fmt.Println(currentTime()+"Error sending ICMP packet: ", er)
			}
		}()

		go func() {
			defer wgX.Done()
			reply := make([]byte, 1500)

			conn.SetReadDeadline(time.Now().Add(c.Timeout))
			_, _, err := conn.ReadFrom(reply)
			if err != nil {
				fmt.Println(currentTime()+"Error receiving reply: ", err)
				RetryICMP++
				if RetrySerial <= LIMIT {
					fmt.Println(currentTime() + "Retrying icmp ping")
				} else {
					FLAG_ICMP = &FALSE
					fmt.Println(currentTime() + "Limit retry reached, error in icmp ping")
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
			fmt.Println(currentTime()+"dbPing|Error dialing: ", err)
			RetryDB++
			if RetryDB <= LIMIT {
				fmt.Println(currentTime() + "dbPing|Retrying to ping db")
				time.Sleep(1 * time.Second)
			} else {
				FLAG_PORT_REMOTE = &FALSE
			}
		} else {
			FLAG_PORT_REMOTE = &TRUE
			//fmt.Println(currentTime()+"Successfully connected to ", c.HostRemote+":"+c.PortRemote)
		}
		conn.Close()

		time.Sleep(20 * time.Millisecond)
	}
	fmt.Printf(currentTime()+"Stopping ping db.... returning flag dbping: %v\n", *FLAG_PORT_REMOTE)

	return
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
		fmt.Println(currentTime()+DB_LOCAL+"|Connect success ", serialName)
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
			fmt.Println("-------------------------------------------")
			fmt.Println(currentTime()+DB_LOCAL+"|ErrorSerial sending data: ", err)
			RetrySerial++
			if RetrySerial <= LIMIT {
				fmt.Println(currentTime() + DB_LOCAL + "|Trying to reconnect......")
				fmt.Printf(currentTime()+DB_LOCAL+"|Attempting retry %v.....\n", RetrySerial)
			}
		} else {
			//fmt.Println("standby baca")

			//Standby akan menunggu respons ack dari master dengan timeout 5 detik
			n, err = readWithTimeout(port, buffer, c.Timeout)

			if err == context.DeadlineExceeded {
				//Jika terjadi timeout, maka akan mengirim ulang ping
				fmt.Println(currentTime() + DB_LOCAL + "|TimeoutSerial reached while waiting for response......")
				RetrySerial++
				if RetrySerial <= LIMIT {
					fmt.Println(currentTime() + DB_LOCAL + "|Trying to resend ping....")
					fmt.Printf(currentTime()+DB_LOCAL+"|Attempting retry %v.....\n", RetrySerial)
				}
			} else {
				//Jika ada respon dari master
				resp := string(buffer[:n])
				if err != nil || !strings.Contains(resp, "ack") {
					fmt.Println(currentTime()+DB_LOCAL+"|ErrorSerial receiving data: ", err, resp, n)
					RetrySerial++
					if RetrySerial <= LIMIT {
						fmt.Println(currentTime() + DB_LOCAL + "|Trying to resend the data......")
						fmt.Printf(currentTime()+DB_LOCAL+"|Attempting retry %v.....\n", RetrySerial)
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
	fmt.Printf(currentTime()+"Stopping Serial ping, returning flag serial: %v\n", *FLAG_SERIAL)
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
		fmt.Println(currentTime()+DB_LOCAL+"|Error connecting serial port: ", err)
	}
	defer port.Close()
	//fmt.Printf("port: %v\n", port)

	// var n int
	buffer := make([]byte, 100)
	for RetrySerial <= LIMIT {
		//Alur loop di master : nunggu req - terima req - kirim ack - nunggu req.....

		// fmt.Println("master baca")
		//Master akan selalu menunggu request dari standby
		_, err = port.Read(buffer)
		if err != nil {
			fmt.Println(currentTime()+DB_LOCAL+"|ErrorSerial receiving data: ", err)
			RetrySerial++
			if RetrySerial <= LIMIT {
				fmt.Println(currentTime() + DB_LOCAL + "|Waiting data....")
				fmt.Printf(currentTime()+DB_LOCAL+"|Attempting retry %v.....\n", RetrySerial)
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
				fmt.Println(currentTime()+DB_LOCAL+"|ErrorSerial sending data: ", err)
				RetrySerial++
				if RetrySerial <= LIMIT {
					fmt.Println(currentTime() + DB_LOCAL + "|Resend data....")
					fmt.Printf(currentTime()+DB_LOCAL+"|Attempting retry %v.....\n", RetrySerial)
				}
			} else {
				RetrySerial = 0
			}

		}

		//Kembali ke awal, menunggu request
	}
	//Master akan memberikan false jika dilakukan retry lebih dari 5 kali dan masih error
	FLAG_SERIAL = &FALSE
	fmt.Printf(currentTime()+"Stopping ping Serial, returning flag serial: %v\n", *FLAG_SERIAL)
}

// dbType mengecek jenis db lokal apakah replica atau master
func (c *Controller) dbType(dbDriver string, wg *sync.WaitGroup) {

	conn, err := sql.Open(dbDriver, c.connStrDB)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	queryRep := fmt.Sprint("SELECT usename FROM pg_stat_replication")

	queryType := fmt.Sprint("SELECT pg_is_in_recovery()")

	var res string
	err = conn.QueryRow(queryType).Scan(&res)

	//Check if it is master or replica
	if err != nil {
		fmt.Println(currentTime()+"dbType|Error executing query: ", err)
	} else {
		if res == "true" {
			DB_LOCAL = "replica"
		} else if res == "false" {
			DB_LOCAL = "master"

			// Check if the replication is working in local db master
			err = conn.QueryRow(queryRep).Scan(&res)

			if err != nil {
				if err == sql.ErrNoRows {
					fmt.Println(currentTime()+"dbType|Something is wrong with the replication: ", err)
					REP_STAT = false
				} else {
					fmt.Println(currentTime()+"dbType|Error executing query: ", err)
				}
			} else {
				REP_STAT = true
			}
		}

		fmt.Printf(currentTime()+"dbType|This database is a %s, replication stat: %v\n", DB_LOCAL, REP_STAT)
	}

}

// dbCheck melakukan pengecekan db lokal untuk mengetahui apakah up atau tidak
func (c *Controller) dbCheck(driverDB string, wg *sync.WaitGroup) {
	defer wg.Done()

	for (RetryDBLcl <= 1) && ((FLAG_SHUTDOWN == nil) && (FLAG_PING_ERR == nil)) {
		conn, err := sql.Open(driverDB, c.connStrDB)
		if err != nil {
			fmt.Println(currentTime()+"dbCheck|Error connecting to database: ", err)
			RetryDBLcl++
			if RetryDBLcl <= 1 {
				fmt.Println(currentTime() + "dbCheck|Trying to restart postgres service")
				restartPostgres()
			} else {
				FLAG_DB_LOCAL = &FALSE
				FLAG_SHUTDOWN = &TRUE
			}
		} else {
			var res int

			query := fmt.Sprint("SELECT 1")

			err = conn.QueryRow(query).Scan(&res)
			// if Count > 9 {
			// 	res = -1
			// }
			if err != nil || res != 1 {
				fmt.Println(currentTime()+"dbCheck|Error querying to database: ", err)
				RetryDBLcl++
				if RetryDBLcl <= 1 {
					fmt.Println(currentTime() + "dbCheck|Trying to restart postgres service")
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
	fmt.Printf(currentTime()+"Stopping check localdb.... returning flag db: %v\n", *FLAG_DB_LOCAL)
}

func (c *Controller) netCheck(wg *sync.WaitGroup) {
	defer wg.Done()

	gateway, err := getDefaultGateway()
	if err != nil {
		fmt.Println(currentTime()+"Error: ", err)
	}

	fmt.Println("Default gateway: ", gateway)

	// ipAddr, err := net.ResolveIPAddr("ip4", gateway)
	// if err != nil {
	// 	fmt.Println(currentTime()+"Error: ", err)
	// }

	// fmt.Println("Resolved IP Address: ", ipAddr)

	for FLAG_SHUTDOWN == nil {
		conn, err := net.DialTimeout("ip4:icmp", gateway, c.Timeout)
		if err != nil {
			fmt.Println(currentTime()+"Error connect to gateway: ", err)
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
		fmt.Printf(currentTime()+"~~Error restarting postgresql: %s ~~\n", err)
		return

	}

	fmt.Println(currentTime() + "~~Successfully restarting postgresql~~")

	return
}

func (c *Controller) promoteStandby() error {
	conn, err := sql.Open("postgres", c.connStrDB)
	if err != nil {
		fmt.Println("Error connecting to db for promote: ", err)
		return err
	}

	if DB_LOCAL == "replica" {
		query := fmt.Sprint("SELECT pg_promote()")
		_, err := conn.Exec(query)
		if err != nil {
			fmt.Println("Error executing query for promote: ", err)
			return err
		}
	}
	return nil
}

func (c *Controller) setVIP() {
	ifaceName := "enp0s3"
	netmask := 24

	cidr := fmt.Sprintf("%s/%d", c.vip, netmask)
	ipNet, err := netlink.ParseIPNet(cidr)
	if err != nil {
		fmt.Println("Error parsing ipNet: ", err)
		return
	}

	link, er := netlink.LinkByName(ifaceName)
	if er != nil {
		fmt.Println("Error getting network interface: ", err)
		return
	}

	addr := &netlink.Addr{IPNet: ipNet, Label: ""}
	err = netlink.AddrAdd(link, addr)
	if err != nil {
		fmt.Println("Error adding virtual IP address: ", err)
		return
	}

	fmt.Printf("Virtual IP %s was added to interface %s \n", c.vip, ifaceName)
	return
}

func getDefaultGateway() (string, error) {
	var gateway string

	switch runtime.GOOS {
	case "darwin":
		gateway = "/sbin/netstat -nr | /usr/bin/grep default | /usr/bin/awk '{print $2}'"
	case "linux":
		gateway = "/sbin/ip route | /usr/bin/awk '/default/ { print $3 }'"
	default:
		return "", fmt.Errorf("Unsupported operating system: %s", runtime.GOOS)
	}

	cmd := exec.Command("sh", "-c", gateway)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), err
}

func currentTime() string {
	current := time.Now().Format("15:04:05.000000|")
	return current

}
