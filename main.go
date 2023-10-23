package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"
	"trygo/controller"
)

func main() {

	fmt.Println("Reading configuration file: %s", os.Args[1])

	jsonFile, err := os.Open("./conf/" + os.Args[1] + ".conf")
	if err != nil {
		fmt.Println(err, "os.Open>")
		time.Sleep(100 * time.Millisecond)
		return
	}

	byteValue, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		fmt.Println(err, "Failed to read config: (%s)")
		time.Sleep(100 * time.Millisecond)
		return
	}

	var jsonconf map[string]interface{}
	err = json.Unmarshal(byteValue, &jsonconf)
	if err != nil {
		fmt.Println(err, "Failed to read config: (%s)")
		time.Sleep(100 * time.Millisecond)
		return
	}

	role := jsonconf["role"].(string)
	connstr := jsonconf["connlocal"].(string)
	timeout := 5 * time.Second
	ipaddr := jsonconf["ipremote"].(string)

	c := controller.NewController(role, connstr, timeout, ipaddr)
	c.Run()
	//c.RunSerial()
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
