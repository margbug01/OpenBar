package lang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every Chinese text the code hands T or F has its English, taking the same
// arguments.
func TestEnglishForEveryText(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0]*[0-9.*]*[a-zA-Z%]`)
	seen := 0
	filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "signalbot" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "T" && sel.Sel.Name != "F" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "lang" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(lit.Value)
			seen++
			e, ok := en[s]
			switch {
			case !ok:
				t.Errorf("%s: no English for %q", fset.Position(lit.Pos()), s)
			case sel.Sel.Name == "F" && !slices.Equal(sortedVerbs(verbs, s), sortedVerbs(verbs, e)):
				t.Errorf("%s: %q and %q take different arguments", fset.Position(lit.Pos()), s, e)
			}
			return true
		})
		return nil
	})
	if seen == 0 {
		t.Fatal("found no texts")
	}
}

// sortedVerbs are the verbs of a format, sorted: English may put them in
// another order only with explicit indexes, %[2]s, which this reads as one
// verb.
func sortedVerbs(re *regexp.Regexp, s string) []string {
	v := re.FindAllString(regexp.MustCompile(`\[\d+\]`).ReplaceAllString(s, ""), -1)
	slices.Sort(v)
	return v
}

func TestSet(t *testing.T) {
	defer Set("")
	for _, c := range []struct {
		tag  string
		want bool
	}{{"zh-Hans", false}, {"zh-Hant", false}, {"en", true}, {"", false}} {
		Set(c.tag)
		if English() != c.want {
			t.Errorf("Set(%q): English %v", c.tag, English())
		}
	}
	Set("en")
	if T("取消") != "Cancel" || T("比特币之外的名字") != "比特币之外的名字" {
		t.Errorf("T in English: %q, %q", T("取消"), T("比特币之外的名字"))
	}
}
