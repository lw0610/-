package main

import "flag"

var buildVersion = "dev"

func main() {
	flag.Parse()
	runDeepAgent()
}
