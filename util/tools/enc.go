package main

import (
	"crypto/aes"
	"crypto/cipher"
	"io/ioutil"
	"os"
	"path/filepath"
)

func encryptData(password []byte, iv []byte, plaintext []byte) (ciphertext []byte) {
	c, err := aes.NewCipher(password)
	if err != nil {
		panic(err)
	}

	dec := cipher.NewCBCEncrypter(c, iv)

	ciphertext = make([]byte, len(plaintext))
	dec.CryptBlocks(ciphertext, plaintext)

	return
}

func main() {
	var err error

	var password []byte
	password, err = ioutil.ReadFile("password")
	if err != nil {
		panic(err)
	}

	var iv []byte
	iv, err = ioutil.ReadFile("iv")
	if err != nil {
		panic(err)
	}

	filename := os.Args[1]
	var plaintext []byte
	plaintext, err = ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	ciphertext := encryptData(password, iv, plaintext)

	// write out files:
	outpath := filepath.Base(filename) + ".enc"
	err = ioutil.WriteFile(outpath, ciphertext, 0644)
	if err != nil {
		panic(err)
	}
}
