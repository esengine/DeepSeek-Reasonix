package checkenv

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/shellrun"
)

func TestContainerScopeEnforcesDeclaredBoundary(t *testing.T) {
	spec := containerFixture(t)
	for path, body := range map[string]string{"go.mod": "module boundary\n\ngo 1.26.0\n", "boundary_test.go": containerBoundaryProbe} {
		if err := os.WriteFile(filepath.Join(spec.SourceRoot, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	private := filepath.Join(spec.SourceRoot, "private")
	if err := os.WriteFile(private, []byte("forbidden input"), 0600); err != nil {
		t.Fatal(err)
	}
	command := `GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=0 GOCACHE=/tmp/go-cache go test -count=1 -v ./...`
	i := NewEnvironment(spec, command, "boundary-execution", "boundary-call")
	t.Cleanup(i.Close)
	argv, err := i.Prepare(t.Context(), tool.ControlledShellRequest{Command: command, Directory: spec.Directory, ReadDenies: []string{private}})
	if err != nil {
		t.Fatal(err)
	}
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: argv, Dir: spec.Directory, Command: command, Track: true})
	inputs := i.Inputs()
	if result.Err != nil || len(inputs) != 1 || !Verify(inputs[0], "boundary-execution") {
		t.Fatalf("container boundary: %v %s %+v", result.Err, result.Combined, inputs)
	}
}

const containerBoundaryProbe = `package boundary
import("net";"os";"strings";"testing")
func TestBoundary(t *testing.T){
 if os.Getuid()!=65534||os.Geteuid()!=65534{t.Fatal("payload retained root identity")}
 interfaces,err:=net.Interfaces();if err!=nil{t.Fatal(err)}
 for _,network:=range interfaces{if network.Name!="lo"{t.Fatal("payload retained external network",network.Name)}}
 for _,path:=range []string{"private","/var/run/docker.sock","/proc/1/fd/0","/run/reasonix-control/status","/proc/1/root/run/reasonix-control/status"}{
  f,err:=os.Open(path);if err==nil{f.Close();t.Fatal("payload accessed host control or forbidden input",path)}
 }
 status,err:=os.ReadFile("/proc/self/status");if err!=nil{t.Fatal(err)}
 for _,line:=range strings.Split(string(status),"\n"){
  fields:=strings.Fields(line);if len(fields)!=2{continue}
  if strings.HasPrefix(fields[0],"Cap")&&fields[1]!="0000000000000000"{t.Fatal("payload retained capability",line)}
  if fields[0]=="NoNewPrivs:"&&fields[1]!="1"{t.Fatal("payload can gain privileges")}
 }
 if err:=os.WriteFile("input",[]byte("overwrite"),0600);err==nil{t.Fatal("payload changed frozen source")}
 if err:=os.WriteFile("/tmp/payload-work",[]byte("temporary"),0600);err!=nil{t.Fatal("payload lacks private working storage",err)}
}
`
