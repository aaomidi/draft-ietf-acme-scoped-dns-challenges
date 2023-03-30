package main

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
)

func CalculateLabel(accountURI string) string {
	x := sha256.Sum256([]byte(accountURI))
	str := base32.StdEncoding.EncodeToString(x[0:10])
	return str
}

func main() {
	fmt.Println(CalculateLabel("https://example.com/acme/acct/ExampleAccount"))
}
