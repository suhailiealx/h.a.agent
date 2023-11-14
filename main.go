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

	fmt.Printf("Reading configuration file: %s\n", os.Args[1])

	jsonFile, err := os.Open(os.Args[1] + ".conf")
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

	timeout := 5 * time.Second
	lclhost := jsonconf["dblclhost"].(string)
	lclport := jsonconf["dblclport"].(string)
	lcluser := jsonconf["dblcluser"].(string)
	lclpass := jsonconf["dblclpass"].(string)
	lclname := jsonconf["dblclname"].(string)

	rmthost := jsonconf["dbrmthost"].(string)
	rmtport := jsonconf["dbrmtport"].(string)

	serialname := jsonconf["serialname"].(string)
	vip := jsonconf["vip"].(string)

	connlcl := paramToConn(lclhost, lclport, lcluser, lclpass, lclname)

	c := controller.NewController(timeout, connlcl, rmthost, rmtport, serialname, vip)
	c.Run()
	//c.RunSerial()
}

func paramToConn(lclhost string, lclport string, lcluser string, lclpass string, lclname string) string {

	conn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=disable", lcluser, lclpass, lclname, lclhost, lclport)

	return conn
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
