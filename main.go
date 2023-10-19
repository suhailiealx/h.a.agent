package main

import (
	"time"
	"trygo/controller"
)

func main() {
	connstr := "user=postgres password=alxius dbname=recordings host=localhost port=5433 sslmode=disable"
	timeout := 5 * time.Second
	ipaddr := "localhost:5432"

	c := controller.NewController("c1", connstr, timeout, ipaddr)
	// c.Run()
	c.RunSerial()
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
