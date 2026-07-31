package toml_test

import (
	"bytes"
	"fmt"
	"log"
	"net/mail"
	"time"

	"github.com/BurntSushi/toml"
)

func ExampleEncoder_Encode() {
	var (
		date, _ = time.Parse(time.RFC822, "14 Mar 10 18:00 UTC")
		buf     = new(bytes.Buffer)
	)
	err := toml.NewEncoder(buf).Encode(map[string]any{
		"date":   date,
		"counts": []int{1, 1, 2, 3, 5, 8},
		"hash": map[string]string{
			"key1": "val1",
			"key2": "val2",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(buf.String())

	// Output:
	// counts = [1, 1, 2, 3, 5, 8]
	// date = 2010-03-14T18:00:00Z
	//
	// [hash]
	//   key1 = "val1"
	//   key2 = "val2"
}

func ExampleMetaData_PrimitiveDecode() {
	tomlBlob := `
		ranking = ["Springsteen", "J Geils"]

		[bands.Springsteen]
		started = 1973
		albums = ["Greetings", "WIESS", "Born to Run", "Darkness"]

		[bands."J Geils"]
		started = 1970
		albums = ["The J. Geils Band", "Full House", "Blow Your Face Out"]
		`

	type (
		band struct {
			Started int
			Albums  []string
		}
		classics struct {
			Ranking []string
			Bands   map[string]toml.Primitive
		}
	)

	// Do the initial decode; reflection is delayed on Primitive values.
	var music classics
	md, err := toml.Decode(tomlBlob, &music)
	if err != nil {
		log.Fatal(err)
	}

	// MetaData still includes information on Primitive values.
	fmt.Printf("Is `bands.Springsteen` defined? %v\n",
		md.IsDefined("bands", "Springsteen"))

	// Decode primitive data into Go values.
	for _, artist := range music.Ranking {
		// A band is a primitive value, so we need to decode it to get a real
		// `band` value.
		primValue := music.Bands[artist]

		var aBand band
		err = md.PrimitiveDecode(primValue, &aBand)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s started in %d.\n", artist, aBand.Started)
	}

	// Check to see if there were any fields left undecoded. Note that this
	// won't be empty before decoding the Primitive value!
	fmt.Printf("Undecoded: %q\n", md.Undecoded())

	// Output:
	// Is `bands.Springsteen` defined? true
	// Springsteen started in 1973.
	// J Geils started in 1970.
	// Undecoded: []
}

func ExampleDecode() {
	tomlBlob := `
		# Some comments.
		[alpha]
		ip = "10.0.0.1"

			[alpha.config]
			Ports = [ 8001, 8002 ]
			Location = "Toronto"
			Created = 1987-07-05T05:45:00Z

		[beta]
		ip = "10.0.0.2"

			[beta.config]
			Ports = [ 9001, 9002 ]
			Location = "New Jersey"
			Created = 1887-01-05T05:55:00Z
	`

	type (
		serverConfig struct {
			Ports    []int
			Location string
			Created  time.Time
		}
		server struct {
			IP     string       `toml:"ip,omitempty"`
			Config serverConfig `toml:"config"`
		}
		servers map[string]server
	)

	var config servers
	_, err := toml.Decode(tomlBlob, &config)
	if err != nil {
		log.Fatal(err)
	}

	for _, name := range []string{"alpha", "beta"} {
		s := config[name]
		fmt.Printf("Server: %s (ip: %s) in %s created on %s\n",
			name, s.IP, s.Config.Location,
			s.Config.Created.Format("2006-01-02"))
		fmt.Printf("Ports: %v\n", s.Config.Ports)
	}

	// Output:
	// Server: alpha (ip: 10.0.0.1) in Toronto created on 1987-07-05
	// Ports: [8001 8002]
	// Server: beta (ip: 10.0.0.2) in New Jersey created on 1887-01-05
	// Ports: [9001 9002]
}

type address struct{ *mail.Address }

func (a *address) UnmarshalText(text []byte) error {
	var err error
	a.Address, err = mail.ParseAddress(string(text))
	return err
}

// Example Unmarshaler shows how to decode TOML strings into your own
// custom data type.
func Example_unmarshaler() {
	blob := `
		contacts = [
			"Donald Duck <donald@duckburg.com>",
			"Scrooge McDuck <scrooge@duckburg.com>",
		]
	`

	var contacts struct {
		// Implementation of the address type:
		//
		//     type address struct{ *mail.Address }
		//
		//     func (a *address) UnmarshalText(text []byte) error {
		//         var err error
		//         a.Address, err = mail.ParseAddress(string(text))
		//         return err
		//     }

		Contacts []address
	}

	_, err := toml.Decode(blob, &contacts)
	if err != nil {
		log.Fatal(err)
	}

	for _, c := range contacts.Contacts {
		fmt.Printf("%#v\n", c.Address)
	}

	// Output:
	// &mail.Address{Name:"Donald Duck", Address:"donald@duckburg.com"}
	// &mail.Address{Name:"Scrooge McDuck", Address:"scrooge@duckburg.com"}
}

// Example StrictDecoding shows how to detect if there are keys in the TOML
// document that weren't decoded into the value given. This is useful for
// returning an error to the user if they've included extraneous fields in their
// configuration.
func Example_strictDecoding() {
	var blob = `
		key1 = "value1"
		key2 = "value2"
		key3 = "value3"
	`

	var conf struct {
		Key1 string
		Key3 string
	}
	md, err := toml.Decode(blob, &conf)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Undecoded keys: %q\n", md.Undecoded())
	// Output:
	// Undecoded keys: ["key2"]
}

type order struct {
	// NOTE `order.parts` is a private slice of type `part` which is an
	// interface and may only be loaded from toml using the UnmarshalTOML()
	// method of the Unmarshaler interface.
	parts parts
}

type parts []part

type part interface {
	Name() string
}

type valve struct {
	Type   string
	ID     string
	Size   float32
	Rating int
}

func (v *valve) Name() string {
	return fmt.Sprintf("VALVE: %s", v.ID)
}

type pipe struct {
	Type     string
	ID       string
	Length   float32
	Diameter int
}

func (p *pipe) Name() string {
	return fmt.Sprintf("PIPE: %s", p.ID)
}

type cable struct {
	Type   string
	ID     string
	Length int
	Rating float32
}

func (c *cable) Name() string {
	return fmt.Sprintf("CABLE: %s", c.ID)
}

func (o *order) UnmarshalTOML(data any) error {
	// NOTE the example below contains detailed type casting to show how the
	// 'data' is retrieved. In operational use, a type cast wrapper may be
	// preferred e.g.
	//
	// func AsMap(v any) (map[string]any, error) {
	// 		return v.(map[string]any)
	// }
	//
	// resulting in:
	// d, _ := AsMap(data)
	//

	d, _ := data.(map[string]any)
	parts, _ := d["parts"].([]map[string]any)

	for _, p := range parts {

		typ, _ := p["type"].(string)
		id, _ := p["id"].(string)

		// detect the type of part and handle each case
		switch p["type"] {
		case "valve":

			size := float32(p["size"].(float64))
			rating := int(p["rating"].(int64))

			valve := &valve{
				Type:   typ,
				ID:     id,
				Size:   size,
				Rating: rating,
			}

			o.parts = append(o.parts, valve)

		case "pipe":

			length := float32(p["length"].(float64))
			diameter := int(p["diameter"].(int64))

			pipe := &pipe{
				Type:     typ,
				ID:       id,
				Length:   length,
				Diameter: diameter,
			}

			o.parts = append(o.parts, pipe)

		case "cable":

			length := int(p["length"].(int64))
			rating := float32(p["rating"].(float64))

			cable := &cable{
				Type:   typ,
				ID:     id,
				Length: length,
				Rating: rating,
			}

			o.parts = append(o.parts, cable)

		}
	}

	return nil
}

// Example UnmarshalTOML shows how to implement a struct type that knows how to
// unmarshal itself. The struct must take full responsibility for mapping the
// values passed into the struct. The method may be used with interfaces in a
// struct in cases where the actual type is not known until the data is
// examined.
func Example_unmarshalTOML() {
	blob := `
		[[parts]]
		type = "valve"
		id = "valve-1"
		size = 1.2
		rating = 4

		[[parts]]
		type = "valve"
		id = "valve-2"
		size = 2.1
		rating = 5

		[[parts]]
		type = "pipe"
		id = "pipe-1"
		length = 2.1
		diameter = 12

		[[parts]]
		type = "cable"
		id = "cable-1"
		length = 12
		rating = 3.1
	`

	// See example_test.go in the source for the implementation of the order
	// type.
	o := &order{}

	err := toml.Unmarshal([]byte(blob), o)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(len(o.parts))
	for _, part := range o.parts {
		fmt.Println(part.Name())
	}
}
