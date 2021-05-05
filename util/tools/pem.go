package main

import (
	"encoding/pem"
	"io/ioutil"
	"log"
	"os"
)

func main() {
	log.SetOutput(os.Stderr)

	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("in = %d\n", len(b))
	block, _ := pem.Decode(b)
	log.Printf("out = %d\n", len(block.Bytes))

	_, err = os.Stdout.Write(block.Bytes)
	if err != nil {
		log.Fatal(err)
	}
}
