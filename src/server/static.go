package server

import (
	"fmt"
	"os"
	"path"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
)

type StaticLoader struct {
	rootDir   string
	templates map[string]*template.Template
}

func NewStaticLoader(rootDir string) (*StaticLoader, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("nil root dir for static loader")
	}
	s := &StaticLoader{
		rootDir:   rootDir,
		templates: make(map[string]*template.Template),
	}
	files, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read files in directory: %v", err)
	}
	for _, f := range files {
		name := path.Join(rootDir, f.Name())
		if !strings.HasSuffix(f.Name(), ".html") {
			log.Warningf("Directory has file not of HTML format, so skipping: %v", name)
			continue
		}
		tmpl, err := template.ParseFiles(name)
		if err != nil {
			return nil, fmt.Errorf("cannot parse file %s: %v", name, err)
		}
		s.templates[f.Name()] = tmpl
	}
	return s, nil
}

func (s *StaticLoader) Portfolio() *template.Template {
	return s.templates["portfolio.html"]
}

func (s *StaticLoader) Accounts() *template.Template {
	return s.templates["accounts.html"]
}
