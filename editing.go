package main

import (
	"bytes"
	"fmt"
	"os"
)

type confirmation struct {
	message string
	actions []action
}

func (a *app) dirtyDocs() []*doc {
	var docs []*doc
	for _, d := range a.docs {
		if d.buf != nil && d.buf.Modified() {
			docs = append(docs, d)
		}
	}
	return docs
}

// guardDirty holds a destructive action until the user saves or discards.
func (a *app) guardDirty(docs []*doc, proceed func()) bool {
	if a.allowDirty || len(docs) == 0 {
		return false
	}
	a.showWelcome = false
	a.confirm = &confirmation{message: fmt.Sprintf("%d file(s) have unsaved changes.", len(docs))}
	a.confirm.actions = []action{
		{"Save", func() {
			for _, d := range docs {
				if d.buf.Modified() && !a.saveDoc(d, false) {
					return
				}
			}
			a.confirm = nil
			proceed()
		}},
		{"Discard Changes", func() {
			a.confirm = nil
			a.allowDirty = true
			defer func() { a.allowDirty = false }()
			proceed()
		}},
		{"Cancel", func() { a.confirm = nil }},
	}
	return true
}

func (a *app) saveActive() {
	if a.focus != nil {
		if d := a.docs[a.focus.active]; d != nil {
			a.saveDoc(d, false)
		}
	}
}

func (a *app) saveAll() {
	for _, d := range a.dirtyDocs() {
		if !a.saveDoc(d, false) {
			return
		}
	}
}

// saveDoc checks the last-read disk contents before writing, including the BOM.
func (a *app) saveDoc(d *doc, overwrite bool) bool {
	if d.buf == nil || d.lossy {
		return false
	}
	if !d.buf.Modified() && !overwrite {
		return true
	}
	current, err := os.ReadFile(d.path)
	if err != nil && !os.IsNotExist(err) {
		a.note("Could not save "+d.name+": "+err.Error(), true)
		return false
	}
	expected := []byte(d.diskText)
	if d.bom {
		expected = append([]byte{0xef, 0xbb, 0xbf}, expected...)
	}
	if !overwrite && (err != nil || !bytes.Equal(current, expected)) {
		d.conflict = true
		a.note("The file changed on disk. Review it before choosing Overwrite.", true)
		return false
	}
	var out bytes.Buffer
	if d.bom {
		out.Write([]byte{0xef, 0xbb, 0xbf})
	}
	if _, err = d.buf.WriteTo(&out); err == nil {
		err = os.WriteFile(d.path, out.Bytes(), 0666)
	}
	if err != nil {
		a.note("Could not save "+d.name+": "+err.Error(), true)
		return false
	}
	d.seq++ // an older asynchronous read must not replace the saved text
	d.loading = false
	d.buf.MarkSaved()
	d.diskText = string(out.Bytes())
	if d.bom {
		d.diskText = d.diskText[3:]
	}
	if fi, err := os.Stat(d.path); err == nil {
		d.stamp = fileStamp{fi.Size(), fi.ModTime()}
	}
	d.gone, d.conflict = false, false
	a.note("Saved "+d.name, false)
	return true
}
