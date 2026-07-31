package selfupdate

import (
	"io/ioutil"
	"testing"
)

func TestMinisign(t *testing.T) {
	v := NewVerifier()
	err := v.LoadFromFile("LICENSE.minisig", "RWQhjNB8gjlNDQYRsRiGEzKTtGwzkcFLRMiSEy+texbTAVMvsgFLLfSr")
	if err != nil {
		t.Fatal(err)
	}

	buf, err := ioutil.ReadFile("LICENSE")
	if err != nil {
		t.Fatal(err)
	}

	if err = v.Verify(buf); err != nil {
		t.Fatal(err)
	}
}
