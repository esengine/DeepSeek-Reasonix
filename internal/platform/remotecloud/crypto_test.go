package remotecloud

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestControllerAndDeviceDeriveTheSameAuthenticatedChannel(t *testing.T) {
	device, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	deviceID := "d-device"
	greeting, _ := json.Marshal(hello{
		Version: protocolVersion, Type: "hello",
		PublicKey: base64.RawURLEncoding.EncodeToString(controller.PublicKey().Bytes()),
		Salt:      base64.RawURLEncoding.EncodeToString(salt),
	})
	deviceCipher, err := newSessionCipher(device, deviceID, string(greeting))
	if err != nil {
		t.Fatal(err)
	}
	controllerGreeting, _ := json.Marshal(hello{
		Version: protocolVersion, Type: "hello",
		PublicKey: base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes()),
		Salt:      base64.RawURLEncoding.EncodeToString(salt),
	})
	controllerCipher, err := newSessionCipher(controller, deviceID, string(controllerGreeting))
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := controllerCipher.seal(map[string]any{"v": 1, "type": "ping", "id": "one"})
	if err != nil {
		t.Fatal(err)
	}
	var command controllerCommand
	if err := deviceCipher.open(sealed, &command); err != nil {
		t.Fatal(err)
	}
	if command.Type != "ping" || command.ID != "one" {
		t.Fatalf("command = %+v", command)
	}
	if err := deviceCipher.open(sealed, &command); err == nil {
		t.Fatal("replayed ciphertext was accepted")
	}
}
