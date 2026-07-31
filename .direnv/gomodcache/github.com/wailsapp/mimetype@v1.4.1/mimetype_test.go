package mimetype

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testDataDir = "testdata"

// test files sorted by the file name in alphabetical order.
var files = map[string]string{
	"3g2.3g2":            "video/3gpp2",
	"3gp.3gp":            "video/3gpp",
	"3mf.3mf":            "application/vnd.ms-package.3dmanufacturing-3dmodel+xml",
	"7z.7z":              "application/x-7z-compressed",
	"a.a":                "application/x-archive",
	"aac.aac":            "audio/aac",
	"aaf.aaf":            "application/octet-stream",
	"accdb.accdb":        "application/x-msaccess",
	"aiff.aiff":          "audio/aiff",
	"amf.amf":            "application/x-amf",
	"amr.amr":            "audio/amr",
	"ape.ape":            "audio/ape",
	"apng.png":           "image/vnd.mozilla.apng",
	"asf.asf":            "video/x-ms-asf",
	"atom.atom":          "application/atom+xml",
	"au.au":              "audio/basic",
	"avi.avi":            "video/x-msvideo",
	"avif.avif":          "image/avif",
	"avifsequence.avif":  "image/avif",
	"bmp.bmp":            "image/bmp",
	"bpg.bpg":            "image/bpg",
	"bz2.bz2":            "application/x-bzip2",
	"cab.cab":            "application/vnd.ms-cab-compressed",
	"class.class":        "application/x-java-applet",
	"crx.crx":            "application/x-chrome-extension",
	"csv.csv":            "text/csv",
	"cpio.cpio":          "application/x-cpio",
	"dae.dae":            "model/vnd.collada+xml",
	"dbf.dbf":            "application/x-dbf",
	"dcm.dcm":            "application/dicom",
	"deb.deb":            "application/vnd.debian.binary-package",
	"djvu.djvu":          "image/vnd.djvu",
	"doc.doc":            "application/msword",
	"docx.1.docx":        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"docx.docx":          "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"drpm.rpm":           "application/x-rpm",
	"dwg.1.dwg":          "image/vnd.dwg",
	"dwg.dwg":            "image/vnd.dwg",
	"eot.eot":            "application/vnd.ms-fontobject",
	"epub.epub":          "application/epub+zip",
	"exe.exe":            "application/vnd.microsoft.portable-executable",
	"fdf.fdf":            "application/vnd.fdf",
	"fits.fits":          "application/fits",
	"flac.flac":          "audio/flac",
	"flv.flv":            "video/x-flv",
	"gbr.gbr":            "image/x-gimp-gbr",
	"geojson.1.geojson":  "application/geo+json",
	"geojson.geojson":    "application/geo+json",
	"gif.gif":            "image/gif",
	"glb.glb":            "model/gltf-binary",
	"gml.gml":            "application/gml+xml",
	"gpx.gpx":            "application/gpx+xml",
	"gz.gz":              "application/gzip",
	"har.har":            "application/json",
	"hdr.hdr":            "image/vnd.radiance",
	"heic.single.heic":   "image/heic",
	"heif.heif":          "image/heif",
	"html.html":          "text/html; charset=utf-8",
	"html.iso88591.html": "text/html; charset=iso-8859-1",
	"html.svg.html":      "text/html; charset=utf-8",
	"html.usascii.html":  "text/html; charset=us-ascii",
	"html.utf8.html":     "text/html; charset=utf-8",
	"html.utf8bom.html":  "text/html; charset=utf-8",
	"html.withbr.html":   "text/html; charset=utf-8",
	"ico.ico":            "image/x-icon",
	"ics.dos.ics":        "text/calendar",
	"ics.ics":            "text/calendar",
	"iso88591.txt":       "text/plain; charset=iso-8859-1",
	"jar.jar":            "application/jar",
	"jp2.jp2":            "image/jp2",
	"jpf.jpf":            "image/jpx",
	"jpg.jpg":            "image/jpeg",
	"jpm.jpm":            "image/jpm",
	"jxl.jxl":            "image/jxl",
	"xpm.xpm":            "image/x-xpixmap",
	"js.js":              "application/javascript",
	"json.json":          "application/json",
	"json.lowascii.json": "application/json",
	// json.{int,float,string}.txt contain a single JSON value. They are valid JSON
	// documents, but they should not be detected as application/json. This mimics
	// the behaviour of the file utility and seems the correct thing to do.
	"json.int.txt":       "text/plain; charset=utf-8",
	"json.float.txt":     "text/plain; charset=utf-8",
	"json.string.txt":    "text/plain; charset=utf-8",
	"kml.kml":            "application/vnd.google-earth.kml+xml",
	"lit.lit":            "application/x-ms-reader",
	"ln":                 "application/x-executable",
	"lua.lua":            "text/x-lua",
	"lz.lz":              "application/lzip",
	"m3u.m3u":            "application/vnd.apple.mpegurl",
	"m4a.m4a":            "audio/x-m4a",
	"audio.mp4":          "audio/mp4",
	"lnk.lnk":            "application/x-ms-shortcut",
	"macho.macho":        "application/x-mach-binary",
	"mdb.mdb":            "application/x-msaccess",
	"midi.midi":          "audio/midi",
	"mkv.mkv":            "video/x-matroska",
	"mobi.mobi":          "application/x-mobipocket-ebook",
	"mov.mov":            "video/quicktime",
	"mp3.mp3":            "audio/mpeg",
	"mp3.v1.notag.mp3":   "audio/mpeg",
	"mp3.v2.5.notag.mp3": "audio/mpeg",
	"mp3.v2.notag.mp3":   "audio/mpeg",
	"mp4.1.mp4":          "video/mp4",
	"mp4.mp4":            "video/mp4",
	"mpc.mpc":            "audio/musepack",
	"mpeg.mpeg":          "video/mpeg",
	"mqv.mqv":            "video/quicktime",
	"mrc.mrc":            "application/marc",
	"msi.msi":            "application/x-ms-installer",
	"msg.msg":            "application/vnd.ms-outlook",
	"ndjson.xl.ndjson":   "application/x-ndjson",
	"ndjson.ndjson":      "application/x-ndjson",
	"nes.nes":            "application/vnd.nintendo.snes.rom",
	"elfobject":          "application/x-object",
	"odf.odf":            "application/vnd.oasis.opendocument.formula",
	"sxc.sxc":            "application/vnd.sun.xml.calc",
	"odg.odg":            "application/vnd.oasis.opendocument.graphics",
	"odp.odp":            "application/vnd.oasis.opendocument.presentation",
	"ods.ods":            "application/vnd.oasis.opendocument.spreadsheet",
	"odt.odt":            "application/vnd.oasis.opendocument.text",
	"ogg.oga":            "audio/ogg",
	"ogg.ogv":            "video/ogg",
	"ogg.spx.oga":        "audio/ogg",
	"otf.otf":            "font/otf",
	"otg.otg":            "application/vnd.oasis.opendocument.graphics-template",
	"otp.otp":            "application/vnd.oasis.opendocument.presentation-template",
	"ots.ots":            "application/vnd.oasis.opendocument.spreadsheet-template",
	"ott.ott":            "application/vnd.oasis.opendocument.text-template",
	"odc.odc":            "application/vnd.oasis.opendocument.chart",
	"owl2.owl":           "application/owl+xml",
	"pat.pat":            "image/x-gimp-pat",
	"pdf.pdf":            "application/pdf",
	"php.php":            "text/x-php",
	"pl.pl":              "text/x-perl",
	"png.png":            "image/png",
	"ppt.ppt":            "application/vnd.ms-powerpoint",
	"pptx.pptx":          "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"ps.ps":              "application/postscript",
	"psd.psd":            "image/vnd.adobe.photoshop",
	"p7s_pem.p7s":        "application/pkcs7-signature",
	"p7s_der.p7s":        "application/pkcs7-signature",
	"pub.pub":            "application/vnd.ms-publisher",
	"py.py":              "application/x-python",
	"qcp.qcp":            "audio/qcelp",
	"rar.rar":            "application/x-rar-compressed",
	"rmvb.rmvb":          "application/vnd.rn-realmedia-vbr",
	"rpm.rpm":            "application/x-rpm",
	"rss.rss":            "application/rss+xml",
	"rtf.rtf":            "text/rtf",
	"sample32.macho":     "application/x-mach-binary",
	"sample64.macho":     "application/x-mach-binary",
	"shp.shp":            "application/octet-stream",
	"shx.shx":            "application/octet-stream",
	"so.so":              "application/x-sharedlib",
	"sqlite.sqlite":      "application/vnd.sqlite3",
	"srt.srt":            "application/x-subrip",
	// not.srt.txt uses periods instead of commas for the decimal separators of
	// the timestamps.
	"not.srt.txt": "text/plain; charset=utf-8",
	// not.srt.2.txt does not specify milliseconds.
	"not.srt.2.txt":   "text/plain; charset=utf-8",
	"svg.1.svg":       "image/svg+xml",
	"svg.svg":         "image/svg+xml",
	"swf.swf":         "application/x-shockwave-flash",
	"tar.tar":         "application/x-tar",
	"tar.v7.tar":      "application/x-tar",
	"tcl.tcl":         "text/x-tcl",
	"tcx.tcx":         "application/vnd.garmin.tcx+xml",
	"tiff.tiff":       "image/tiff",
	"torrent.torrent": "application/x-bittorrent",
	"tsv.tsv":         "text/tab-separated-values",
	"ttc.ttc":         "font/collection",
	"ttf.ttf":         "font/ttf",
	"tzfile":          "application/tzif",
	"utf16bebom.txt":  "text/plain; charset=utf-16be",
	"utf16lebom.txt":  "text/plain; charset=utf-16le",
	"utf32bebom.txt":  "text/plain; charset=utf-32be",
	"utf32lebom.txt":  "text/plain; charset=utf-32le",
	"utf8.txt":        "text/plain; charset=utf-8",
	"utf8ctrlchars":   "application/octet-stream",
	"vcf.dos.vcf":     "text/vcard",
	"vcf.vcf":         "text/vcard",
	"voc.voc":         "audio/x-unknown",
	"vtt.vtt":         "text/vtt",
	"vtt.space.vtt":   "text/vtt",
	"vtt.tab.vtt":     "text/vtt",
	"vtt.eof.vtt":     "text/vtt",
	"warc.warc":       "application/warc",
	"wasm.wasm":       "application/wasm",
	"wav.wav":         "audio/wav",
	"webm.webm":       "video/webm",
	"webp.webp":       "image/webp",
	"woff.woff":       "font/woff",
	"woff2.woff2":     "font/woff2",
	"x3d.x3d":         "model/x3d+xml",
	"xar.xar":         "application/x-xar",
	"xcf.xcf":         "image/x-xcf",
	"xfdf.xfdf":       "application/vnd.adobe.xfdf",
	"xlf.xlf":         "application/x-xliff+xml",
	"xls.xls":         "application/vnd.ms-excel",
	"xlsx.1.xlsx":     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"xlsx.2.xlsx":     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"xlsx.xlsx":       "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"xml.xml":         "text/xml; charset=utf-8",
	"xml.withbr.xml":  "text/xml; charset=utf-8",
	"xz.xz":           "application/x-xz",
	"zip.zip":         "application/zip",
	"zst.zst":         "application/zstd",
}

func TestDetect(t *testing.T) {
	errStr := "File: %s; Expected: %s != Detected: %s; err: %v"
	extStr := "File: %s; ExpectedExt: %s != DetectedExt: %s"
	for fName, expected := range files {
		fileName := filepath.Join(testDataDir, fName)
		f, err := os.Open(fileName)
		if err != nil {
			t.Fatal(err)
		}
		data, err := ioutil.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}

		if mtype := Detect(data); mtype.String() != expected {
			t.Errorf(errStr, fName, expected, mtype.String(), nil)
		}

		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}

		if mtype, err := DetectReader(f); mtype.String() != expected {
			t.Errorf(errStr, fName, expected, mtype.String(), err)
		}
		f.Close()

		if mtype, err := DetectFile(fileName); mtype.String() != expected {
			t.Errorf(errStr, fName, expected, mtype.String(), err)
		} else if mtype.Extension() != filepath.Ext(fName) {
			t.Errorf(extStr, fName, filepath.Ext(fName), mtype.Extension())
		}
	}
}

// This test generates the doc file containing the table with the supported MIMEs.
func TestGenerateSupportedFormats(t *testing.T) {
	f, err := os.OpenFile("supported_mimes.md", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	nodes := root.flatten()
	header := fmt.Sprintf(`## %d Supported MIME types
This file is automatically generated when running tests. Do not edit manually.

Extension | MIME type | Aliases
--------- | --------- | -------
`, len(nodes))

	if _, err := f.WriteString(header); err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		ext := n.extension
		if ext == "" {
			ext = "n/a"
		}

		aliases := strings.Join(n.aliases, ", ")
		if aliases == "" {
			aliases = "-"
		}
		str := fmt.Sprintf("**%s** | %s | %s\n", ext, n.mime, aliases)
		if _, err := f.WriteString(str); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEqualsAny(t *testing.T) {
	type ss []string
	testCases := []struct {
		m1  string
		m2  ss
		res bool
	}{
		{"foo/bar", ss{"foo/bar"}, true},
		{"  foo/bar", ss{"foo/bar	"}, true}, // whitespace
		{"  foo/bar", ss{"foo/BAR	"}, true}, // case
		{"  foo/bar", ss{"foo/baz"}, false},
		{";charset=utf-8", ss{""}, true},
		{"", ss{"", "foo/bar"}, true},
		{"foo/bar", ss{""}, false},
		{"foo/bar", nil, false},
	}
	for _, tc := range testCases {
		if EqualsAny(tc.m1, tc.m2...) != tc.res {
			t.Errorf("Equality test failed for %+v", tc)
		}
	}
}

func TestDetectReader(t *testing.T) {
	errStr := "File: %s; Mime: %s != DetectedMime: %s; err: %v"
	for fName, expected := range files {
		fileName := filepath.Join(testDataDir, fName)
		f, err := os.Open(fileName)
		if err != nil {
			t.Fatal(err)
		}
		r := breakReader{
			r:         f,
			breakSize: 3,
		}
		if mtype, err := DetectReader(&r); mtype.String() != expected {
			t.Errorf(errStr, fName, expected, mtype.String(), err)
		}
		f.Close()
	}
}

// breakReader breaks the string every breakSize characters.
// It is like:
//   <html><h
//   ead><tit
//   le>html<
//   ...
type breakReader struct {
	r         io.Reader
	breakSize int
}

func (b *breakReader) Read(p []byte) (int, error) {
	if len(p) > b.breakSize {
		p = p[:b.breakSize]
	}
	n, err := io.ReadFull(b.r, p)
	if err == io.ErrUnexpectedEOF {
		return n, io.EOF
	}
	return n, err
}

func TestFaultyInput(t *testing.T) {
	inexistent := "inexistent.file"
	if mtype, err := DetectFile(inexistent); err == nil {
		t.Errorf("%s should not match successfully", inexistent)
	} else if mtype.String() != "application/octet-stream" {
		t.Errorf("inexistent.file expected application/octet-stream, got %s", mtype)
	}

	f, _ := os.Open(inexistent)
	if mtype, err := DetectReader(f); err == nil {
		t.Errorf("%s reader should not match successfully", inexistent)
	} else if mtype.String() != "application/octet-stream" {
		t.Errorf("inexistent.file reader expected application/octet-stream, got %s", mtype)
	}
}

func TestHierarchy(t *testing.T) {
	detectedMIME, err := DetectFile("testdata/html.html")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"text/html; charset=utf-8",
		"text/plain",
		"application/octet-stream",
	}

	got := []string{}
	for mtype := detectedMIME; mtype != nil; mtype = mtype.Parent() {
		got = append(got, mtype.String())
	}
	if le, lg := len(expected), len(got); le != lg {
		t.Fatalf("hierarchy len error; expected: %d, got: %d", le, lg)
	}

	for i := range expected {
		if expected[i] != got[i] {
			t.Fatalf("hierarchy error; expected: %s, got: %s", expected, got)
		}
	}
}

func TestConcurrent(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(4)

	go func() {
		for i := 0; i < 1000; i++ {
			Detect([]byte("text content"))
		}
		wg.Done()
	}()
	go func() {
		for i := 0; i < 1000; i++ {
			SetLimit(5000 + uint32(i))
		}
		wg.Done()
	}()
	go func() {
		for i := 0; i < 1000; i++ {
			Lookup("text/plain")
		}
		wg.Done()
	}()
	go func() {
		for i := 0; i < 1000; i++ {
			Extend(func([]byte, uint32) bool { return false }, "e", ".e")
			Lookup("text/plain").Extend(func([]byte, uint32) bool { return false }, "e", ".e")
		}
		wg.Done()
	}()

	wg.Wait()
	// Reset to original limit for benchmarks.
	SetLimit(3072)
}

// For #162.
func TestEmptyInput(t *testing.T) {
	mtype, err := DetectReader(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("empty reader err; expected: nil, got: %s", err)
	}
	plain := "text/plain"
	if !mtype.Is(plain) {
		t.Fatalf("empty reader detection; expected: %s, got: %s", plain, mtype)
	}
	mtype = Detect(nil)
	if !mtype.Is(plain) {
		t.Fatalf("empty bytes slice detection; expected: %s, got: %s", plain, mtype)
	}
	SetLimit(0)
	mtype, err = DetectReader(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("0 limіt, empty reader err; expected: nil, got: %s", err)
	}
	if !mtype.Is(plain) {
		t.Fatalf("0 limit, empty reader detection; expected: %s, got: %s", plain, mtype)
	}
	SetLimit(3072)
}

// Benchmarking a random slice of bytes is as close as possible to the real
// world usage. A random byte slice is almost guaranteed to fail being detected.
//
// When performing a detection on a file it is very likely there will be
// multiple rules failing before finding the one that matches, ex: a jpg file
// might be tested for zip, gzip, etc., before it is identified.
func BenchmarkSliceRand(b *testing.B) {
	r := rand.New(rand.NewSource(0))
	data := make([]byte, 3072)
	if _, err := io.ReadFull(r, data); err != io.ErrUnexpectedEOF && err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Detect(data)
		}
	})
}

func BenchmarkCommon(b *testing.B) {
	commonFiles := map[string]string{
		"tar":  "testdata/tar.tar",
		"zip":  "testdata/zip.zip",
		"pdf":  "testdata/pdf.pdf",
		"jpg":  "testdata/jpg.jpg",
		"png":  "testdata/png.png",
		"gif":  "testdata/gif.gif",
		"xls":  "testdata/xls.xls",
		"webm": "testdata/webm.webm",
		"xlsx": "testdata/xlsx.xlsx",
		"pptx": "testdata/pptx.pptx",
		"docx": "testdata/docx.docx",
	}
	for k, v := range commonFiles {
		b.Run(k, func(b *testing.B) {
			f, err := ioutil.ReadFile(v)
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				Detect(f)
			}
		})
	}
}

// Check there are no panics for nil inputs.
func TestIndexOutOfRangePanic(t *testing.T) {
	for _, n := range root.flatten() {
		n.detector(nil, 1<<10)
	}
}

// MIME type equality ignores any optional MIME parameters, so, in order to not
// parse each alias when testing for equality, we must ensure they are
// registered with no parameters.
func TestMIMEFormat(t *testing.T) {
	for _, n := range root.flatten() {
		// All extensions must be dot prefixed so they are compatible
		// with the stdlib mime package.
		if n.Extension() != "" && !strings.HasPrefix(n.Extension(), ".") {
			t.Fatalf("extension %s should be dot prefixed", n.Extension())
		}
		// All MIMEs must be correctly formatted.
		_, _, err := mime.ParseMediaType(n.String())
		if err != nil {
			t.Fatalf("error parsing node MIME: %s", err)
		}
		// Aliases must have no optional MIME parameters.
		for _, a := range n.aliases {
			parsed, params, err := mime.ParseMediaType(a)
			if err != nil {
				t.Fatalf("error parsing node alias MIME: %s", err)
			}
			if parsed != a || len(params) > 0 {
				t.Fatalf("node alias MIME should have no optional params; alias: %s, params: %v", a, params)
			}
		}
	}
}

func TestLookup(t *testing.T) {
	data := []struct {
		mime string
		m    *MIME
	}{
		{root.mime, root},
		{zip.mime, zip},
		{zip.aliases[0], zip},
		{xlsx.mime, xlsx},
	}

	for _, tt := range data {
		t.Run(fmt.Sprintf("lookup %s", tt.mime), func(t *testing.T) {
			if m := Lookup(tt.mime); m != tt.m {
				t.Fatalf("failed to lookup: %s", tt.mime)
			}
		})
	}
}

func TestExtend(t *testing.T) {
	data := []struct {
		mime   string
		ext    string
		parent *MIME
	}{
		{"foo", ".foo", nil},
		{"bar", ".bar", root},
		{"baz", ".baz", zip},
	}

	for _, tt := range data {
		t.Run(fmt.Sprintf("extending to %s", tt.mime), func(t *testing.T) {
			extend := Extend
			if tt.parent != nil {
				extend = tt.parent.Extend
			} else {
				tt.parent = root
			}

			extend(func(raw []byte, limit uint32) bool { return false }, tt.mime, tt.ext)
			m := Lookup(tt.mime)
			if m == nil {
				t.Fatalf("mime %s not found", tt.mime)
			}
			if m.parent != tt.parent {
				t.Fatalf("mime %s has wrong parent: want %s, got %s", tt.mime, tt.parent.mime, m.parent.mime)
			}
		})
	}
}
