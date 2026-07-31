package jsonschema_test

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type HTTPURLLoader http.Client

func (l *HTTPURLLoader) Load(url string) (any, error) {
	client := (*http.Client)(l)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s returned status code %d", url, resp.StatusCode)
	}
	defer resp.Body.Close()

	return jsonschema.UnmarshalJSON(resp.Body)
}

func newHTTPURLLoader(insecure bool) *HTTPURLLoader {
	httpLoader := HTTPURLLoader(http.Client{
		Timeout: 15 * time.Second,
	})
	if insecure {
		httpLoader.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return &httpLoader
}

func Example_fromHTTPS() {
	schemaURL := "https://raw.githubusercontent.com/santhosh-tekuri/boon/main/tests/examples/schema.json"
	instanceFile := "./testdata/examples/instance.json"

	loader := jsonschema.SchemeURLLoader{
		"file":  jsonschema.FileLoader{},
		"http":  newHTTPURLLoader(false),
		"https": newHTTPURLLoader(false),
	}

	c := jsonschema.NewCompiler()
	c.UseLoader(loader)
	sch, err := c.Compile(schemaURL)
	if err != nil {
		log.Fatal(err)
	}

	f, err := os.Open(instanceFile)
	if err != nil {
		log.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		log.Fatal(err)
	}

	err = sch.Validate(inst)
	fmt.Println("valid:", err == nil)
	// Output:
	// valid: true
}
