package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/tarm/serial"
)

var RetryDB int
var RetrySerial int
var Count int
var FLAG_DB *bool
var FLAG_SERIAL *bool
var DB_LOCAL string
var REP_STAT bool

var TRUE = true
var FALSE = false

func main() {
	timeout := 5 * time.Second
	connStrLocal := "user=postgres password=alxius dbname=recordings host=localhost port=5433 sslmode=disable"

	var wg sync.WaitGroup
	var mu sync.Mutex

	portCheck("gpnode2.cchosting.my.id:25569", timeout)
	dbType(connStrLocal, "postgres", &wg)

	wg.Add(1)
	go dbCheck(connStrLocal, "postgres", &wg)
	if DB_LOCAL == "master" {
		wg.Add(1)
		go serialMaster("/dev/pts/9", timeout, &wg, &mu)
	} else if DB_LOCAL == "replica" {
		wg.Add(2)
		go serialStandby("/dev/pts/10", timeout, &wg, &mu)
		go waitFlag(&wg, &mu)
	}
	wg.Wait()

	fmt.Println("Exiting main...")
}

// waitFlag melakukan print status flag
func waitFlag(wg *sync.WaitGroup, mu *sync.Mutex) {
	defer wg.Done()
	for {
		fmt.Println("===========================================")
		if FLAG_DB == nil && FLAG_SERIAL == nil {
			fmt.Println(currentTime() + "Waiting for both flags to be ready.....")
		} else if FLAG_DB != nil && FLAG_SERIAL != nil {

			if *FLAG_DB && *FLAG_SERIAL {
				fmt.Printf(currentTime()+"FLAG_DB: %v | FLAG_SERIAL: %v \n", *FLAG_DB, *FLAG_SERIAL)
				fmt.Println(currentTime() + "Connection is stable, continue monitoring.....")
			} else if !*FLAG_DB && !*FLAG_SERIAL {
				fmt.Println(currentTime() + "Connection error from master, promoting.....")
				break
			} else {
				fmt.Println(currentTime() + "Something is wrong!!")
				if !*FLAG_DB {
					fmt.Printf(currentTime()+"FLAG_DB : %v\n", *FLAG_DB)
				}
				if !*FLAG_SERIAL {
					fmt.Printf(currentTime()+"FLAG_SERIAL : %v\n", *FLAG_SERIAL)
				}
			}

		} else {
			fmt.Println(currentTime() + "Waiting for results.....")
			if FLAG_DB != nil && !*FLAG_DB {
				fmt.Printf(currentTime()+"FLAG_DB : %v\n", *FLAG_DB)
			}
			if FLAG_SERIAL != nil && !*FLAG_SERIAL {
				fmt.Printf(currentTime()+"FLAG_SERIAL : %v\n", *FLAG_SERIAL)
			}
		}
		fmt.Println("===========================================")
		time.Sleep(10 * time.Second)
	}
}

// dbType mengecek jenis db lokal apakah replica atau master
func dbType(connStrLocal string, dbDriver string, wg *sync.WaitGroup) {

	conn, err := sql.Open(dbDriver, connStrLocal)
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

// serialStandby mengirimkan sebuah req/write ke master dan menunggu ack dari master, bila tidak ada maka akan diulangi hingga jumlah tertentu
func serialStandby(serialName string, timeout time.Duration, wg *sync.WaitGroup, mu *sync.Mutex) {
	defer wg.Done()

	config := &serial.Config{
		Name:        serialName,
		Baud:        9600,
		ReadTimeout: timeout,
	}

	port, err := serial.OpenPort(config)
	if err != nil {
		//fmt.Println("SerialPingStandby|Error connecting serial port: ", err)
		panic(err)
	}

	defer port.Close()

	buffer := make([]byte, 100)
	var n int
	for RetrySerial <= 5 {
		//Alur loop di standby : kirim req - nunggu ack - terima ack - kirim req.....

		dataToSend := []byte("Serial ping")
		//fmt.Println("standby write")
		//Standby mengirimkan request/ping ke master
		_, err = port.Write(dataToSend)
		if err != nil {
			fmt.Println("-------------------------------------------")
			fmt.Println(currentTime()+"Standby|ErrorSerial sending data: ", err)
			RetrySerial++
			if RetrySerial <= 5 {
				fmt.Println(currentTime() + "Standby|Trying to reconnect......")
				fmt.Printf(currentTime()+"Standby|Attempting retry %v.....\n", RetrySerial)
				fmt.Println("-------------------------------------------")
			}
		} else {

			time.Sleep(3 * time.Second)

			//fmt.Println("standby baca")

			//Standby akan menunggu respons ack dari master dengan timeout 5 detik
			n, err = readWithTimeout(port, buffer, timeout)

			if err == context.DeadlineExceeded {
				//Jika terjadi timeout, maka akan mengirim ulang ping
				fmt.Println("-------------------------------------------")
				fmt.Println(currentTime() + "Standby|TimeoutSerial reached while waiting for response......")
				RetrySerial++
				if RetrySerial <= 5 {
					fmt.Println(currentTime() + "Standby|Trying to resend ping....")
					fmt.Printf(currentTime()+"Standby|Attempting retry %v.....\n", RetrySerial)
					fmt.Println("-------------------------------------------")
				}
			} else {
				//Jika ada respon dari master
				resp := string(buffer[:n])
				if err != nil || !strings.Contains(resp, "ack") {
					fmt.Println("-------------------------------------------")
					fmt.Println(currentTime()+"Standby|ErrorSerial receiving data: ", err, resp)
					RetrySerial++
					if RetrySerial <= 5 {
						fmt.Println(currentTime() + "Standby|Trying to resend the data......")
						fmt.Printf(currentTime()+"Standby|Attempting retry %v.....\n", RetrySerial)
						fmt.Println("-------------------------------------------")
					}
				} else {
					//Respons ack berhasil diterima, memberikan true
					fmt.Println("-------------------------------------------")
					fmt.Printf(currentTime()+"Standby|DataSerial received: %s\n", string(buffer[:n]))
					RetrySerial = 0
					mu.Lock()
					FLAG_SERIAL = &TRUE
					fmt.Println(currentTime() + "Standby|Serial ping success: " + fmt.Sprint(*FLAG_SERIAL))
					fmt.Println("-------------------------------------------")
					mu.Unlock()
					time.Sleep(3 * time.Second)
				}
			}

		}

	}
	//Standby akan memberikan false jika telah retry lebih dari 5 kali dan masih error
	FLAG_SERIAL = &FALSE
	fmt.Println("-------------------------------------------")
	fmt.Printf(currentTime()+"Stopping Serial ping, returning flag serial: %v\n", *FLAG_SERIAL)
	fmt.Println("-------------------------------------------")
}

func readWithTimeout(port *serial.Port, buffer []byte, timeout time.Duration) (int, error) {
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
func serialMaster(serialName string, timeout time.Duration, wg *sync.WaitGroup, mu *sync.Mutex) {
	defer wg.Done()

	config := &serial.Config{
		Name: serialName,
		Baud: 9600,
	}

	port, err := serial.OpenPort(config)
	if err != nil {
		fmt.Println(currentTime()+"Master|Error connecting serial port: ", err)
	}
	defer port.Close()

	var n int
	buffer := make([]byte, 100)
	for RetrySerial <= 5 {
		//Alur loop di master : nunggu req - terima req - kirim ack - nunggu req.....

		//fmt.Println("master baca")
		//Master akan selalu menunggu request dari standby
		n, err = port.Read(buffer)
		if err != nil {
			fmt.Println("-------------------------------------------")
			fmt.Println(currentTime()+"Master|ErrorSerial receiving data: ", err)
			RetrySerial++
			if RetrySerial <= 5 {
				fmt.Println(currentTime() + "Master|Waiting data....")
				fmt.Printf(currentTime()+"Master|Attempting retry %v.....\n", RetrySerial)
				fmt.Println("-------------------------------------------")
			}
		} else {

			//Master berhasil mendapatkan request dari standby
			fmt.Println("-------------------------------------------")
			fmt.Printf(currentTime()+"Master|DataSerial received: %s\n", string(buffer[:n]))
			fmt.Println("-------------------------------------------")

			dataToSend := []byte("ack")
			//fmt.Println("master write")
			//Master mengirimkan respon ack kepada standby sebagai tanda bahwa request telah diterima
			n, err = port.Write(dataToSend)
			if err != nil {
				fmt.Println("-------------------------------------------")
				fmt.Println(currentTime()+"Master|ErrorSerial sending data: ", err)
				RetrySerial++
				if RetrySerial <= 5 {
					fmt.Println(currentTime() + "Master|Resend data....")
					fmt.Printf(currentTime()+"Master|Attempting retry %v.....\n", RetrySerial)
					fmt.Println("-------------------------------------------")
				}
			} else {
				RetrySerial = 0
			}

		}

		//Kembali ke awal, menunggu request
	}
	//Master akan memberikan false jika dilakukan retry lebih dari 5 kali dan masih error
	FLAG_SERIAL = &FALSE
	fmt.Println("-------------------------------------------")
	fmt.Printf(currentTime()+"Stopping ping Serial, returning flag serial: %v\n", *FLAG_SERIAL)
	fmt.Println("-------------------------------------------")
}

// dbCheck melakukan pengecekan db lokal untuk mengetahui apakah up atau tidak
func dbCheck(connStr string, driverDB string, wg *sync.WaitGroup) {
	defer wg.Done()

	for RetryDB <= 6 {
		conn, err := sql.Open(driverDB, connStr)
		if err != nil {
			fmt.Println(currentTime()+"dbCheck|Error connecting to database: ", err)
			RetryDB++
			if RetryDB <= 5 {
				fmt.Println(currentTime() + "dbCheck|Restarting connection.......")
				fmt.Printf(currentTime()+"dbCheck|Attempting retry %v..... \n", RetryDB)
			} else if RetryDB == 6 {
				fmt.Println(currentTime() + "dbCheck|Trying to restart postgres service")
				restartPostgres()
			}
		} else {
			var res int

			query := fmt.Sprint("SELECT 1")

			err = conn.QueryRow(query).Scan(&res)
			if Count > 15 {
				res = -1
			}
			if err != nil || res != 1 {
				fmt.Println(currentTime()+"dbCheck|Error querying to database: ", err)
				RetryDB++
				if RetryDB <= 5 {
					fmt.Println(currentTime() + "dbCheck|Restarting connection.......")
					fmt.Printf(currentTime()+"dbCheck|Attempting retry %v..... \n", RetryDB)
				} else if RetryDB == 6 {
					fmt.Println(currentTime() + "dbCheck|Trying to restart postgres service")
					restartPostgres()
				}
			} else {
				RetryDB = 0
				FLAG_DB = &TRUE
				fmt.Println(currentTime() + "dbCheck " + fmt.Sprint(*FLAG_DB))
			}
		}
		conn.Close()

		//Interval cek selama 2 detik
		time.Sleep(2 * time.Second)
		Count++

	}
	FLAG_DB = &FALSE
	fmt.Printf(currentTime()+"Stopping ping DB.... returning flag db: %v\n", *FLAG_DB)
}

// portCheck digunakan oleh standby untuk mengecek bila port yang akan digunakan remote db apakah menerima koneksi atau tidak
func portCheck(address string, timeout time.Duration) {

	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		fmt.Println(currentTime()+"Error dialing: ", err)
		return
	}
	defer conn.Close()

	fmt.Println(currentTime()+"Successfully connected to ", address)
}

func restartPostgres() {
	cmd := exec.Command("sudo", "systemctl", "restart", "postgresql")

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		fmt.Printf(currentTime()+"~~Error restarting postgresql: %s ~~\n", err)
		return

	}

	fmt.Println(currentTime() + "~~Successfully restarting postgresql~~")
	return
}

func currentTime() string {
	current := time.Now().Format("15:04:05.000000|")
	return current

}

// func execCommand(command string, port string) (output string) {
// 	cmd := exec.Command(command, "-p", port)
// 	stdout, err := cmd.StdoutPipe()
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	// Start the command.
// 	if err := cmd.Start(); err != nil {
// 		log.Fatal(err)
// 	}

// 	// Read and print the output.
// 	outputBytes, err := io.ReadAll(stdout)
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	outputString := string(outputBytes)

// 	if err := cmd.Wait(); err != nil {
// 		log.Fatal(err)
// 	}
// 	return outputString

// }

// For checking db connection with TCP
// dbAddr := "localhost"
// dbPort := "5432"

// for {
// 	_, err := net.Dial("tcp", dbAddr+":"+dbPort)
// 	if err != nil {
// 		fmt.Println("Error connecting to the database: ", err)
// 	} else {
// 		fmt.Println("Connecting successfully")
// 	}
// 	time.Sleep(2 * time.Second)
// }
