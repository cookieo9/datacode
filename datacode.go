package main

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"flag"
	"go/build"
	"go/format"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

var (
	out      = flag.String("o", "data.go", "Output file")
	prefix   = flag.String("prefix", "", "Prefix to strip from filenames")
	suffix   = flag.String("suffix", "", "Suffix to strip from filenames")
	compress = flag.Bool("compress", true, "Use compression")
	override = flag.String("pkg", "", "Override package name")
	gofmt    = flag.Bool("fmt", true, "Run output through gofmt")
	level    = flag.Int("level", flate.DefaultCompression, "Compression Level")
	force    = flag.Bool("f", false, "Force overwrite of existing file")
)

const tmplText = `package {{ .Package }}
import (
	"encoding/base64"
	"bytes"
	"strings"
	"io"
{{ if .Compress }}
	"compress/flate"
{{ end }}
)
{{ range .Files }}
func {{.Func}} () ([]byte, error) {
	data := "{{.Base64}}"
	b64 := base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))
	out := new(bytes.Buffer)
	{{ if .Compress }}
	r := flate.NewReader(b64)
	if _, err := io.Copy(out,r) ; err != nil {
		return nil, err
	}
	if err := r.Close(); err != nil {
		return nil, err
	}
	{{ else }}
	if _, err := io.Copy(out, b64) ; err != nil {
		return nil, err
	}
	{{ end }}
	return out.Bytes(), nil
}
{{ end }}
`

var tmpl = template.Must(template.New("output").Parse(tmplText))

func doIt(c *config, gofmt bool) ([]byte, error) {
	buf := new(bytes.Buffer)

	if err := tmpl.Execute(buf, c); err != nil {
		return nil, err
	}
	data := buf.Bytes()

	if !gofmt {
		return data, nil
	}
	return format.Source(data)
}

func main() {
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(-1)
	}

	if !*force && exists(*out) {
		log.Fatalf("Can't output, %q exists (use -f to override)", *out)
	}

	p := *override
	if len(p) == 0 {
		odir := filepath.Dir(*out)
		pkg, err := build.ImportDir(odir, 0)
		if err != nil {
			log.Fatal(err)
		}
		p = pkg.Name
	}

	c := &config{
		Package:       p,
		Prefix:        *prefix,
		Suffix:        *suffix,
		Args:          flag.Args(),
		Compress:      *compress,
		CompressLevel: *level,
	}

	data, err := doIt(c, *gofmt)
	if err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile(*out, data, 0644); err != nil {
		log.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}

	if os.IsNotExist(err) {
		return false
	}

	panic(err)
}

type config struct {
	Package        string
	Prefix, Suffix string
	Args           []string
	Compress       bool
	CompressLevel  int
}

type file struct {
	*config
	Path string
}

func (c *config) Files() []file {
	out := make([]file, 0, len(c.Args))
	for _, arg := range c.Args {
		out = append(out, file{Path: arg, config: c})
	}
	return out
}

func (f *file) Func() string {
	name := strings.TrimPrefix(f.Path, f.Prefix)
	name = strings.TrimSuffix(name, f.Suffix)

	rep := func(r rune) rune {
		if unicode.IsDigit(r) || unicode.IsLetter(r) || r > 127 {
			return r
		}
		return '_'
	}

	name = strings.Map(rep, name)
	name = strings.ToLower(name)
	name = strings.Trim(name, "_")

	return name
}

func (f *file) pack(data []byte) ([]byte, error) {
	o := new(bytes.Buffer)
	w, err := flate.NewWriter(o, f.CompressLevel)
	if err != nil {
		return nil, err
	}
	_, err = w.Write(data)
	if err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return o.Bytes(), nil
}

func (f *file) data() ([]byte, error) {
	data, err := ioutil.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	if f.Compress {
		if data, err = f.pack(data); err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (f *file) Base64() (string, error) {
	data, err := f.data()
	if err != nil {
		return "", err
	}
	out := new(bytes.Buffer)
	enc := base64.NewEncoder(base64.StdEncoding, out)
	if _, err := enc.Write(data); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}
