package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"io/ioutil"
	"os"
)

func encryptPassword(rsaPublicKeyBlock []byte, password []byte) (encryptedPassword []byte) {
	// parse the PKCS1 public key:
	rsaPublicKeyIntf, err := x509.ParsePKIXPublicKey(rsaPublicKeyBlock)
	if err != nil {
		panic(err)
	}
	rsaPublicKey := rsaPublicKeyIntf.(*rsa.PublicKey)

	// encrypt the password:
	encryptedPassword, err = rsa.EncryptPKCS1v15(rand.Reader, rsaPublicKey, password)
	if err != nil {
		panic(err)
	}

	return
}

func main() {
	var err error
	var rsaPublicKey []byte
	rsaPublicKey, err = base64.StdEncoding.DecodeString(os.Getenv("RSA_PUBLIC_KEY_BASE64"))

	password := make([]byte, 32)
	_, err = rand.Reader.Read(password)
	if err != nil {
		panic(err)
	}

	iv := make([]byte, 16)
	_, err = rand.Reader.Read(iv)
	if err != nil {
		panic(err)
	}

	encryptedPassword := encryptPassword(rsaPublicKey, password)

	// write out files:
	err = ioutil.WriteFile("password", password, 0600)
	if err != nil {
		panic(err)
	}
	err = ioutil.WriteFile("password.enc", encryptedPassword, 0644)
	if err != nil {
		panic(err)
	}
	outpathIV := "iv"
	err = ioutil.WriteFile(outpathIV, iv, 0644)
	if err != nil {
		panic(err)
	}
}
