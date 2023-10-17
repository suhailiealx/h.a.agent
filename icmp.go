package main

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

var ReplyReceived = make(chan bool)
var Counter int
var randString = map[int]string{0: "Hello ping", 1: "Hello world", 2: "This is ping", 3: "Knock knock", 4: "ICMP is coming"}

func picmp() {
	destinationIP := "127.0.0.1"

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		fmt.Println("Error creating ICMP connection: ", err)
		return
	}
	defer conn.Close()

	destAddr, err := net.ResolveIPAddr("ip4", destinationIP)
	if err != nil {
		fmt.Println("Error resolving IP address: ", err)
		return
	}

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{
			ID: os.Getpid() & 0xffff, Seq: 1,
			Data: []byte("This is ping"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		fmt.Println("Error marshaling ICMP message: ", err)
		return
	}

	var wg sync.WaitGroup

	for {
		startTime := time.Now()

		wg.Add(1)
		go sendICMP(conn, msgBytes, destAddr, startTime, &wg)
		go recICMP(conn, startTime, &wg)
		wg.Wait()

		fmt.Println("bypass")
		select {
		case <-ReplyReceived:
			fmt.Println("Reply received")
			Counter++
		case <-time.After(5 * time.Second):
			fmt.Printf("Timeout:%v --- Server failure, time to promote\n", time.Since(startTime))

		}

		//fmt.Println("Counter : ", Counter)
	}

}

func sendICMP(conn *icmp.PacketConn, msgBytes []byte, destAddr *net.IPAddr, startTime time.Time, wg *sync.WaitGroup) {
	defer wg.Done()
	if Counter < 10 {
		_, er := conn.WriteTo(msgBytes, destAddr)
		fmt.Println("go in sendICMP")
		if er != nil {
			fmt.Println("Error sending ICMP packet: ", er)
		}
	}

}

func recICMP(conn *icmp.PacketConn, startTime time.Time, wg *sync.WaitGroup) {
	reply := make([]byte, 1500)
	n, peer, err := conn.ReadFrom(reply)
	if err == nil {
		msg, err := icmp.ParseMessage(ipv4.ICMPTypeEcho.Protocol(), reply[:n])
		if err == nil {
			fmt.Println("go in recICMP")
			if msg.Type == ipv4.ICMPTypeEchoReply {
				echoReplyData := msg.Body.(*icmp.Echo).Data
				rtt := time.Since(startTime)
				fmt.Printf("Received ICMP Echo Reply from %s (RTT=%v): Data=%s\n", peer, rtt, string(echoReplyData))
				ReplyReceived <- true
			}
		} else {
			fmt.Println("Error parsing ICMP message: ", err)
		}
	}

}
