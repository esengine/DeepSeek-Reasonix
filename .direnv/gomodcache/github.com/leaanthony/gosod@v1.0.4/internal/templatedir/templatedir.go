package templatedir

import (
	"bytes"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
)

// TemplateDir defines a directory containing directories and files, including template files
type TemplateDir struct {
	fs              fs.FS
	templateFilters []string
	dirs            []string
	standardFiles   []string
	templateFiles   []string
	ignoredFiles    map[string]struct{}
	renameFiles     map[string]string
}

// New attempts to create a new TemplateDir from the given FS
func New(fs fs.FS) *TemplateDir {

	return &TemplateDir{
		fs:              fs,
		templateFilters: []string{".tmpl"},
		ignoredFiles:    make(map[string]struct{}),
		renameFiles:     make(map[string]string),
	}

}

// IgnoreFile will add the given filename to the list of files to ignore
// during extraction
func (t *TemplateDir) IgnoreFile(filename string) {
	t.ignoredFiles[filename] = struct{}{}
}

// SetTemplateFilters sets the template filter. Each filename is checked to see if
// it contains this string and if so, it is deemed to be a template file
func (t *TemplateDir) SetTemplateFilters(filters []string) {
	t.templateFilters = filters
}

func (t *TemplateDir) RenameFiles(renameFiles map[string]string) {
	t.renameFiles = renameFiles
}

// Extract the templates to the given directory, using data as input
func (t *TemplateDir) Extract(targetDirectory string, data interface{}) error {

	// Get the absolute path
	targetDirectory, err := filepath.Abs(targetDirectory)
	if err != nil {
		return err
	}

	// If the targetDirectory doesn't exist, then create it
	if _, err := os.Stat(targetDirectory); os.IsNotExist(err) == true {
		// Create the targetDirectory
		err = os.MkdirAll(targetDirectory, 0755)
		if err != nil {
			return err
		}
	}

	// Process the template files
	err = t.processTemplateDirFiles(targetDirectory, data)
	if err != nil {
		return err
	}

	return nil
}

func (t *TemplateDir) processTemplateDirFiles(targetDirectory string, data interface{}) error {
	// Categorise all files
	err := t.categoriseFiles()
	if err != nil {
		return err
	}

	// Create all directories
	err = t.createDirectories(targetDirectory, data)
	if err != nil {
		return err
	}

	// Process TemplateDirs
	err = t.processTemplateDirs(targetDirectory, data)
	if err != nil {
		return err
	}

	// Copy files
	err = t.copyFiles(targetDirectory, data)
	if err != nil {
		return err
	}

	return nil
}

func (t *TemplateDir) categoriseFiles() error {
	return fs.WalkDir(t.fs, ".", t.categoriseFile)
}

func (t *TemplateDir) categoriseFile(path string, info fs.DirEntry, err error) error {

	// Process error
	if err != nil {
		return err
	}

	// Is it a directory?
	if info.IsDir() {
		// Ignore base dir
		if path != "." {
			t.dirs = append(t.dirs, path)
		}
		return nil
	}

	// Get the filename
	filename := filepath.Base(path)

	// Is it a file we are ignoring?
	_, ignored := t.ignoredFiles[filename]
	if ignored {
		return nil
	}

	// Is it a template?
	for _, filter := range t.templateFilters {
		if strings.Index(filename, filter) > -1 {
			t.templateFiles = append(t.templateFiles, path)
			return nil
		}
	}

	// Treat as standard file
	t.standardFiles = append(t.standardFiles, path)
	return nil
}

func (t *TemplateDir) convertPathTarget(path string, targetDirectory string, data any) string {

	result := filepath.Join(targetDirectory, path)
	if data == nil {
		return result
	}

	// Load the filename as a template
	tmpl, err := template.New("filename").Parse(result)
	if err != nil {
		return result
	}

	// Execute the template
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return result
	}

	// Return the result
	return buf.String()
}

func (t *TemplateDir) createDirectories(targetDirectory string, data any) error {

	// Iterate all directories and attempt to create them
	for _, dirPath := range t.dirs {

		targetDir := t.convertPathTarget(dirPath, targetDirectory, data)

		// Create the directory
		err := os.MkdirAll(targetDir, 0755)

		// Ignore directory exists errors
		if err != nil && err != syscall.EEXIST {
			return err
		}
	}

	return nil
}

func (t *TemplateDir) processTemplateDirs(targetDirectory string, data interface{}) error {

	// Iterate template files
	for _, templateFile := range t.templateFiles {

		// Parse template
		tmpl, err := template.ParseFS(t.fs, templateFile)
		if err != nil {
			return err
		}

		// Convert path to target path
		targetFile := t.convertPathTarget(templateFile, targetDirectory, data)

		// update filename
		baseDir := filepath.Dir(targetFile)
		filename := filepath.Base(targetFile)
		for _, filter := range t.templateFilters {
			filename = strings.ReplaceAll(filename, filter, "")
		}
		renamedFile := t.renameFiles[filename]
		if renamedFile != "" {
			filename = renamedFile
		}
		targetFile = filepath.Join(baseDir, filename)

		// Create target file
		writer, err := os.Create(targetFile)
		if err != nil {
			return err
		}

		err = tmpl.Execute(writer, data)
		if err != nil {
			err2 := writer.Close()
			if err2 != nil {
				return err2
			}
			return err
		}

		err = writer.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *TemplateDir) copyFiles(targetDirectory string, data any) error {

	// Iterate over files
	for _, filename := range t.standardFiles {
		targetFile := filename
		renamedFile := t.renameFiles[filename]
		if renamedFile != "" {
			targetFile = renamedFile
		}

		targetFilename := t.convertPathTarget(targetFile, targetDirectory, data)
		err := t.copyFile(filename, targetFilename)
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *TemplateDir) copyFile(source, target string) error {
	s, err := t.fs.Open(source)
	if err != nil {
		return err
	}
	defer func(s fs.File) {
		err := s.Close()
		if err != nil {
			log.Fatal(err)
		}
	}(s)
	d, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(d, s); err != nil {
		err := d.Close()
		if err != nil {
			return err
		}
		return err
	}
	return d.Close()
}
