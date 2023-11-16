package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"h.a.agent/controller"
)

func main() {

	fmt.Printf("Reading properties file: %s\n", os.Args[1])

	jsonFile, err := os.Open(os.Args[1] + ".properties")
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
	lclhost := jsonconf["dblcl_host"].(string)
	lclport := jsonconf["dblcl_port"].(string)
	lcluser := jsonconf["dblcl_user"].(string)
	lclpass := jsonconf["dblcl_pass"].(string)
	lclname := jsonconf["dblcl_name"].(string)

	rmthost := jsonconf["dbrmt_host"].(string)
	rmtport := jsonconf["dbrmt_port"].(string)

	serialname := jsonconf["serial_name"].(string)
	gateway := jsonconf["gateway"].(string)
	email := jsonconf["email"].(string)
	mail_server := jsonconf["mail_server"].(string)
	vip := jsonconf["vip"].(string)

	connlcl := paramToConn(lclhost, lclport, lcluser, lclpass, lclname)

	c := controller.NewController(timeout, connlcl, rmthost, rmtport, serialname, gateway, email, mail_server, vip)
	c.Run()
}

func paramToConn(lclhost string, lclport string, lcluser string, lclpass string, lclname string) string {

	conn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=disable", lcluser, lclpass, lclname, lclhost, lclport)

	return conn
}
