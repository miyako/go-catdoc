package gocatdoc_test

import (
	gocatdoc "github.com/miyako/go-catdoc"
	"io"
	"os"
	"testing"
)

func TestVersion(t *testing.T) {
	version, err := gocatdoc.GetVersion()
	if err != nil {
		t.Fatalf("expected error to be nil, got %v", err)
	}
	if version != "Catdoc Version 0.95" {
		t.Fatalf("expected version to be \"Catdoc Version 0.95\", got %v", version)
	}
}

func testFileFunc(title, expected string, fun func(io.ReadSeeker) (string, error), t *testing.T) {
	f, err := os.Open("test.doc")
	if err != nil {
		t.Fatalf("could not open test document, %v", err)
	}
	text, err := fun(f)
	if err != nil {
		t.Fatalf("expected error to be nil, got %v", err)
	}
	if text != expected {
		t.Fatalf("expected %s to be \"%s\", got %v", title, expected, text)
	}
}

func testFileFuncArr(title string, expected []string, fun func(io.ReadSeeker) ([]string, error), t *testing.T) {
	f, err := os.Open("test.doc")
	if err != nil {
		t.Fatalf("could not open test document, %v", err)
	}
	arr, err := fun(f)
	if err != nil {
		t.Fatalf("expected error to be nil, got %v", err)
	}
	for i := range expected {
		if arr[i] != expected[i] {
			t.Fatalf("expected %s to be \"%v\", got %v", title, expected, arr)
		}
	}
}

func TestGetTextFromFile(t *testing.T) {
	testFileFunc("text", "text-inside-doc", gocatdoc.GetTextFromFile, t)
}

func TestGetAuthorFromFile(t *testing.T) {
	testFileFunc("author", "H. Potter", gocatdoc.GetAuthorFromFile, t)
}

func TestGetLastAuthorFromFile(t *testing.T) {
	testFileFunc("last author", "H. Potter", gocatdoc.GetLastAuthorFromFile, t)
}

func TestGetTitleFromFile(t *testing.T) {
	testFileFunc("title", "Title", gocatdoc.GetTitleFromFile, t)
}

func TestGetSubjectFromFile(t *testing.T) {
	testFileFunc("subject", "Subject", gocatdoc.GetSubjectFromFile, t)
}

func TestGetKeywordsFromFile(t *testing.T) {
	testFileFunc("keywords", "Keywords", gocatdoc.GetKeywordsFromFile, t)
}

func TestGetCommentsFromFile(t *testing.T) {
	testFileFunc("comments", "Comments", gocatdoc.GetCommentsFromFile, t)
}

func TestGetAnnotationAuthorsFromFile(t *testing.T) {
	testFileFuncArr("annoation_authors", []string{"H. Potter"}, gocatdoc.GetAnnotationAuthorsFromFile, t)
}
