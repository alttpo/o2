package alttp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"os"
	"testing"
)

//go:embed "testdata/vt-NoGlitches-open-ganon_7Lyqon5LM2.sfc.enc"
//go:embed "testdata/vt-NoGlitches-open-ganon_7Lyqon5LM2-wram.bin.enc"
//go:embed "testdata/password.enc"
//go:embed "testdata/iv"
var testdata embed.FS

func readEmbed(t *testing.T, path string) (contents []byte) {
	var err error
	contents, err = testdata.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func decryptData(t *testing.T, rsaPrivateKeyBase64 []byte, encryptedPassword []byte, iv []byte, ciphertext []byte) (plaintext []byte) {
	rsaPrivateKeyBlock := make([]byte, len(rsaPrivateKeyBase64))
	n, err := base64.StdEncoding.Decode(rsaPrivateKeyBlock, rsaPrivateKeyBase64)
	if err != nil {
		t.Fatal(err)
	}
	rsaPrivateKeyBlock = rsaPrivateKeyBlock[:n]

	// parse the PKCS8 private key:
	var rsaPrivateKey *rsa.PrivateKey
	rsaPrivateKey, err = x509.ParsePKCS1PrivateKey(rsaPrivateKeyBlock)
	if err != nil {
		t.Fatal(err)
	}

	// decrypt the passwordHex:
	var password []byte
	password, err = rsa.DecryptPKCS1v15(rand.Reader, rsaPrivateKey, encryptedPassword)
	if err != nil {
		t.Fatal(err)
	}

	c, err := aes.NewCipher(password)
	if err != nil {
		t.Fatal(err)
	}

	dec := cipher.NewCBCDecrypter(c, iv)

	plaintext = make([]byte, len(ciphertext))
	dec.CryptBlocks(plaintext, ciphertext)

	return
}

func loadTestData(t *testing.T) (romContents []byte, wramContents []byte) {
	rsaPrivateKeyBase64 := []byte(os.Getenv("RSA_PRIVATE_KEY_BASE64"))
	encryptedPassword := readEmbed(t, "testdata/password.enc")
	iv := readEmbed(t, "testdata/iv")

	// decrypt the ROM file:
	romContents = decryptData(
		t,
		rsaPrivateKeyBase64,
		encryptedPassword,
		iv,
		readEmbed(t, "testdata/vt-NoGlitches-open-ganon_7Lyqon5LM2.sfc.enc"),
	)

	// decrypt WRAM snapshot:
	wramContents = decryptData(
		t,
		rsaPrivateKeyBase64,
		encryptedPassword,
		iv,
		readEmbed(t, "testdata/vt-NoGlitches-open-ganon_7Lyqon5LM2-wram.bin.enc"),
	)

	return
}
