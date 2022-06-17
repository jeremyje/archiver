package archiver

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestPathWithoutTopDir(t *testing.T) {
	for i, tc := range []struct {
		input, expect string
	}{
		{
			input:  "a/b/c",
			expect: "b/c",
		},
		{
			input:  "b/c",
			expect: "c",
		},
		{
			input:  "c",
			expect: "c",
		},
		{
			input:  "",
			expect: "",
		},
	} {
		if actual := pathWithoutTopDir(tc.input); actual != tc.expect {
			t.Errorf("Test %d (input=%s): Expected '%s' but got '%s'", i, tc.input, tc.expect, actual)
		}
	}
}

//go:generate zip testdata/test.zip go.mod
//go:generate zip -qr9 testdata/nodir.zip archiver.go go.mod cmd/arc/main.go .github/ISSUE_TEMPLATE/bug_report.md .github/FUNDING.yml README.md .github/workflows/ubuntu-latest.yml
//go:generate tar --exclude=testdata --exclude=.git -czf testdata/test-repository.tar.gz .
//go:generate tar --exclude=testdata --exclude=.git -cJf testdata/test-repository.tar.xz .
//go:generate tar --exclude=testdata --exclude=.git --exclude=.git -cf testdata/test-repository.tar.lz4 -I 'lz4' .
//go:generate tar --exclude=testdata --exclude=.git -cf testdata/test-repository.tar .
//go:generate tar --exclude=testdata --exclude=.git -cjf testdata/test-repository.tar.bz2 .

var (
	//go:embed testdata/test.zip
	testZIP []byte
	//go:embed testdata/nodir.zip
	nodirZIP []byte
	//go:embed testdata/test-repository.tar.gz
	testRepositoryTarGz []byte
	//go:embed testdata/test-repository.tar.xz
	testRepositoryTarXz []byte
	//go:embed testdata/test-repository.tar.lz4
	testRepositoryTarLz4 []byte
	//go:embed testdata/test-repository.tar
	testRepositoryTar []byte
	//go:embed testdata/test-repository.tar.bz2
	testRepositoryTarBz2 []byte
)

func ExampleArchiveFS_Stream() {
	fsys := ArchiveFS{
		Stream: io.NewSectionReader(bytes.NewReader(testZIP), 0, int64(len(testZIP))),
		Format: Zip{},
	}
	// You can serve the contents in a web server:
	http.Handle("/static", http.StripPrefix("/static",
		http.FileServer(http.FS(fsys))))

	// Or read the files using fs functions:
	dis, err := fsys.ReadDir(".")
	if err != nil {
		log.Fatal(err)
	}
	for _, di := range dis {
		fmt.Println(di.Name())
		b, err := fs.ReadFile(fsys, path.Join(".", di.Name()))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(bytes.Contains(b, []byte("require (")))
	}
	// Output:
	// go.mod
	// true
}

func TestArchiveFS_ReadDir(t *testing.T) {
	for _, tc := range []struct {
		name    string
		archive ArchiveFS
		want    map[string][]string
	}{
		{
			name: "test.zip",
			archive: ArchiveFS{
				Stream: io.NewSectionReader(bytes.NewReader(testZIP), 0, int64(len(testZIP))),
				Format: Zip{},
			},
			// unzip -l testdata/test.zip
			want: map[string][]string{
				".": {"go.mod"},
			},
		},
		{
			name: "nodir.zip",
			archive: ArchiveFS{
				Stream: io.NewSectionReader(bytes.NewReader(nodirZIP), 0, int64(len(nodirZIP))),
				Format: Zip{},
			},
			// unzip -l testdata/nodir.zip
			want: map[string][]string{
				".":       {".github", "README.md", "archiver.go", "cmd", "go.mod"},
				".github": {"FUNDING.yml", "ISSUE_TEMPLATE", "workflows"},
				"cmd":     {"arc"},
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := tc.archive
			for baseDir, wantLS := range tc.want {
				baseDir := baseDir
				wantLS := wantLS
				t.Run(fmt.Sprintf("ReadDir(%s)", baseDir), func(t *testing.T) {
					dis, err := fsys.ReadDir(baseDir)
					if err != nil {
						t.Error(err)
					}

					dirs := []string{}
					for _, di := range dis {
						dirs = append(dirs, di.Name())
					}

					// Stabilize the sort order
					sort.Strings(dirs)

					if !reflect.DeepEqual(wantLS, dirs) {
						t.Errorf("ReadDir() got: %v, want: %v", dirs, wantLS)
					}
				})

				// Uncomment to reproduce https://github.com/mholt/archiver/issues/340.
				/*
					t.Run(fmt.Sprintf("Open(%s)", baseDir), func(t *testing.T) {
						f, err := fsys.Open(baseDir)
						if err != nil {
							t.Error(err)
						}

						rdf, ok := f.(fs.ReadDirFile)
						if !ok {
							t.Fatalf("'%s' did not return a fs.ReadDirFile, %+v", baseDir, rdf)
						}

						dis, err := rdf.ReadDir(-1)
						if err != nil {
							t.Fatal(err)
						}

						dirs := []string{}
						for _, di := range dis {
							dirs = append(dirs, di.Name())
						}

						// Stabilize the sort order
						sort.Strings(dirs)

						if !reflect.DeepEqual(wantLS, dirs) {
							t.Errorf("Open().ReadDir(-1) got: %v, want: %v", dirs, wantLS)
						}
					})
				*/
			}
		})
	}
}

var (
	testRepositoryDirList = map[string][]string{
		".github": {"FUNDING.yml", "ISSUE_TEMPLATE", "SECURITY.md", "workflows"},
		//".github/ISSUE_TEMPLATE": {"bug_report.md", "generic-feature-request.md", "new-format-request.md"},
		//"cmd":                    {"arc"},
		//"cmd/arc":                {"main.go"},
	}
)

func TestFileSystem(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want map[string][]string
	}{
		{
			name: "test.tar.gz",
			data: testRepositoryTarGz,
			// tar tvf testdata/test.tar.gz
			want: testRepositoryDirList,
		},
		/*
			{
				name: "test.tar.xz",
				data: testRepositoryTarXz,
				// tar tvf testdata/test.tar.xz
				want: testRepositoryDirList,
			},
			{
				name: "test.tar.lz4",
				data: testRepositoryTarLz4,
				// tar tvf testdata/test.tar.lz4
				want: testRepositoryDirList,
			},
			{
				name: "test.tar",
				data: testRepositoryTar,
				// tar tvf testdata/test.tar
				want: testRepositoryDirList,
			},
			{
				name: "test.tar.bz2",
				data: testRepositoryTarBz2,
				// tar tvf testdata/test.tar.bz2
				want: testRepositoryDirList,
			},
		*/
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			//t.Parallel()

			tempDir, err := os.MkdirTemp(os.TempDir(), "TestFileSystem")
			t.Cleanup(func() {
				if err := os.RemoveAll(tempDir); err != nil {
					t.Error(err)
				}
			})

			if err != nil {
				t.Fatal(err)
			}

			archivePath := filepath.Join(tempDir, tc.name)
			if err := ioutil.WriteFile(archivePath, tc.data, 0644); err != nil {
				t.Fatal(err)
			}

			fsys, err := FileSystem(archivePath)
			if err != nil {
				t.Fatal(err)
			}

			fsysReadDir, ok := fsys.(fs.ReadDirFS)
			if !ok {
				t.Fatalf("FileSystem(%s) cannot cast to fs.ReadDirFS", archivePath)
			}

			for baseDir, wantLS := range tc.want {
				baseDir := baseDir
				wantLS := wantLS
				t.Run(fmt.Sprintf("ReadDir(%s)", baseDir), func(t *testing.T) {
					dis, err := fsysReadDir.ReadDir(baseDir)
					if err != nil {
						t.Error(err)
					}

					dirs := []string{}
					for _, di := range dis {
						dirs = append(dirs, di.Name())
					}

					// Stabilize the sort order
					sort.Strings(dirs)

					if !reflect.DeepEqual(wantLS, dirs) {
						t.Errorf("ReadDir() got: %v, want: %v", dirs, wantLS)
					}
				})

				// Uncomment to reproduce https://github.com/mholt/archiver/issues/340.
				/*
					t.Run(fmt.Sprintf("Open(%s)", baseDir), func(t *testing.T) {
						f, err := fsys.Open(baseDir)
						if err != nil {
							t.Error(err)
						}
						rdf, ok := f.(fs.ReadDirFile)
						if !ok {
							t.Fatalf("'%s' did not return a fs.ReadDirFile, %+v", baseDir, rdf)
						}
						dis, err := rdf.ReadDir(-1)
						if err != nil {
							t.Fatal(err)
						}
						dirs := []string{}
						for _, di := range dis {
							dirs = append(dirs, di.Name())
						}
						// Stabilize the sort order
						sort.Strings(dirs)
						if !reflect.DeepEqual(wantLS, dirs) {
							t.Errorf("Open().ReadDir(-1) got: %v, want: %v", dirs, wantLS)
						}
					})
				*/
			}
		})
	}
}
