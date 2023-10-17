package main

import (
	"fmt"
	"net"
	"time"
)

func sendHeartBeats(conn net.Conn, heartBeatInterval time.Duration) {
	for {
		message := "message"
		_, err := conn.Write([]byte(message))
		if err != nil {
			fmt.Println("Error sending heartbeat: ", err)
			break
		}
		fmt.Println("Sending heartbeat.....")

		ack := make([]byte, 256)
		_, er := conn.Read(ack)
		if er != nil {
			fmt.Println("Error receiving acknowledgement: ", err)
			break
		}
		fmt.Println("Receiving acknowledgement: ", string(ack))

		time.Sleep(heartBeatInterval)
	}
}

func recHeartBeats(listener net.Listener) {
	for {
		fmt.Println("masuk pertama")
		conn, err := listener.Accept()
		fmt.Println("masuk kedua")
		if err != nil {
			fmt.Println("Error accepting connection: ", err)
			break
		}

		go handleClient(conn)
	}
}

func handleClient(conn net.Conn) {
	defer conn.Close()

	for {
		buff := make([]byte, 256)
		_, er := conn.Read(buff)
		if er != nil {
			panic(er)
		}
		fmt.Println("Receiving heartbeat.....: ", string(buff))

		ack := "acknowledge"
		_, err := conn.Write([]byte(ack))
		if err != nil {
			fmt.Println("Error sending acknowledgement: ", err)
		}
		fmt.Println("Sending acknowledgement....")
	}
}

func heartBeat() {
	var role string
	fmt.Println("Role : ")
	fmt.Scanln(&role)

	address := "localhost:45569"
	if role == "a" {
		heartBeatInterval := 2 * time.Second

		conn, err := net.Dial("tcp", address)
		if err != nil {
			fmt.Println("Error connection to node: ", err)
			return
		}
		defer conn.Close()

		sendHeartBeats(conn, heartBeatInterval)
	} else {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			fmt.Println("Error creating listener: ", err)
			return
		}
		defer listener.Close()

		fmt.Println("Node is listening for heartbeats......")
		recHeartBeats(listener)
	}
}
