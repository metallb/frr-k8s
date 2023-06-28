// SPDX-License-Identifier:Apache-2.0

package main

import (
	"flag"
	"fmt"
	"html/template"
	"os"
	"strings"
)

type BGPD struct {
	NodesIP  []string
	Protocol string
}

func main() {
	nodeList := flag.String("nodes", "", "nodes ip")
	flag.Parse()
	fmt.Println(*nodeList)
	data := BGPD{
		NodesIP:  strings.Split(*nodeList, " "),
		Protocol: "ipv4",
	}

	t, err := template.New("frr.conf.tmpl").ParseFiles("frr.conf.tmpl")
	if err != nil {
		panic(err)
	}
	f, err := os.Create("frr.conf")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	err = t.Execute(f, data)
	if err != nil {
		panic(err)
	}
}
