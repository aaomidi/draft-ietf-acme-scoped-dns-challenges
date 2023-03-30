package main

import (
	"crypto/sha256"
	"encoding/base32"
	"flag"
	"fmt"
)

var label = flag.String("label", "https://example.com/acme/acct/ExampleAccount", "Label used to calculate the DNS-ACCOUNT-01 value with.")

func CalculateLabel(accountURI string) string {
	x := sha256.Sum256([]byte(accountURI))
	str := base32.StdEncoding.EncodeToString(x[0:10])
	return str
}

func main() {
	// Parse the command line flags.
	flag.Parse()
	fmt.Println(CalculateLabel(*label))
}
