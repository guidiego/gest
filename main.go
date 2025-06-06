package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

type TestEvent struct {
	Time    string  `json:"Time"`
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

type SubTest struct {
	Name   string
	Passed bool
	Time   float64
}

type ParentTest struct {
	Name     string
	Subtests []SubTest
	Passed   bool
}

type PackageResult struct {
	Name      string
	Passed    bool
	Skipped   bool
	Duration  float64
	ParentMap map[string]*ParentTest
	HasTests  bool
}

type FileCoverage struct {
	File      string
	Covered   map[int]struct{}
	Uncovered map[int]struct{}
	Total     map[int]struct{}
}

type TreeNode struct {
	Name      string
	IsDir     bool
	Children  map[string]*TreeNode
	Covered   int
	Total     int
	Uncovered []int
	Coverage  float64
}

func parseCoverProfile(path string) (map[string]*FileCoverage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	re := regexp.MustCompile(`^(.+):(\d+)\.\d+,\d+\.\d+ \d+ (\d+)$`)
	result := make(map[string]*FileCoverage)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode:") {
			continue
		}
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		filePath := m[1]
		lineNum, _ := strconv.Atoi(m[2])
		count, _ := strconv.Atoi(m[3])

		if _, ok := result[filePath]; !ok {
			result[filePath] = &FileCoverage{
				File:      filePath,
				Covered:   make(map[int]struct{}),
				Uncovered: make(map[int]struct{}),
				Total:     make(map[int]struct{}),
			}
		}
		result[filePath].Total[lineNum] = struct{}{}
		if count > 0 {
			result[filePath].Covered[lineNum] = struct{}{}
		} else {
			result[filePath].Uncovered[lineNum] = struct{}{}
		}
	}
	return result, scanner.Err()
}

func buildTree(fileData map[string]*FileCoverage) *TreeNode {
	root := &TreeNode{
		Name:     ".",
		IsDir:    true,
		Children: map[string]*TreeNode{},
	}
	for filePath, fc := range fileData {
		parts := strings.Split(filePath, string(filepath.Separator))
		node := root
		for i, part := range parts {
			if _, ok := node.Children[part]; !ok {
				node.Children[part] = &TreeNode{
					Name:     part,
					IsDir:    i < len(parts)-1,
					Children: map[string]*TreeNode{},
				}
			}
			node = node.Children[part]
		}
		node.Covered = len(fc.Covered)
		node.Total = len(fc.Total)
		node.Uncovered = make([]int, 0, len(fc.Uncovered))
		for line := range fc.Uncovered {
			node.Uncovered = append(node.Uncovered, line)
		}
		sort.Ints(node.Uncovered)
		if node.Total > 0 {
			node.Coverage = float64(node.Covered) / float64(node.Total) * 100
		}
	}
	return root
}

func aggregate(node *TreeNode) {
	if !node.IsDir {
		return
	}
	totalCovered := 0
	totalLines := 0
	for _, child := range node.Children {
		aggregate(child)
		totalCovered += child.Covered
		totalLines += child.Total
	}
	node.Covered = totalCovered
	node.Total = totalLines
	if totalLines > 0 {
		node.Coverage = float64(totalCovered) / float64(totalLines) * 100
	}
}

func colorCoverage(coverage float64) text.Colors {
	switch {
	case coverage < 20:
		return text.Colors{text.FgRed}
	case coverage < 50:
		return text.Colors{text.FgHiYellow}
	case coverage < 70:
		return text.Colors{text.FgYellow}
	case coverage < 90:
		return text.Colors{text.FgGreen}
	default:
		return text.Colors{text.FgHiGreen}
	}
}

func addRows(t table.Writer, node *TreeNode, depth int) {
	indent := strings.Repeat("  ", depth)
	name := node.Name
	if node.IsDir && node.Name != "." {
		name += "/"
	}
	displayName := name
	if node.Name != "." {
		displayName = indent + name
	}
	lines := fmt.Sprintf("%d/%d", node.Covered, node.Total)
	coverage := fmt.Sprintf("%6.2f%%", node.Coverage)
	color := colorCoverage(node.Coverage)
	uncovered := ""
	if !node.IsDir && len(node.Uncovered) > 0 {
		strs := make([]string, len(node.Uncovered))
		for i, n := range node.Uncovered {
			strs[i] = strconv.Itoa(n)
		}
		uncovered = strings.Join(strs, ",")
	}
	if node.Name != "." {
		t.AppendRow(
			table.Row{
				color.Sprintf("%s", displayName),
				color.Sprintf("%s", coverage),
				color.Sprintf("%s", lines),
				text.FgRed.Sprintf(uncovered),
			},
		)
	}
	children := make([]*TreeNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		return children[i].Name < children[j].Name
	})
	for _, child := range children {
		addRows(t, child, depth+1)
	}
}

func prettify(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "/", " > ")
	return name
}

func printProgress(testsDone int) {
	// Desenha uma barra de progresso simples
	barLen := 20
	filled := testsDone % (barLen + 1)
	bar := strings.Repeat("■", filled) + strings.Repeat(" ", barLen-filled)
	fmt.Printf("\rRunning tests: [%s] %d tests done", bar, testsDone)
}

func main() {
	coverProfile := flag.String("coverprofile", "", "Path to coverage profile")
	flag.StringVar(coverProfile, "c", "", "Path to coverage profile (shorthand)")
	flag.Parse()

	start := time.Now()
	scanner := bufio.NewScanner(os.Stdin)

	packages := make(map[string]*PackageResult)
	suitesPassed, suitesFailed, suitesSkipped := 0, 0, 0
	testsPassed, testsFailed := 0, 0
	testsDone := 0

	for scanner.Scan() {
		line := scanner.Text()
		var event TestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if _, ok := packages[event.Package]; !ok {
			packages[event.Package] = &PackageResult{
				Name:      event.Package,
				ParentMap: make(map[string]*ParentTest),
			}
		}

		if event.Test != "" {
			packages[event.Package].HasTests = true
			parts := strings.SplitN(event.Test, "/", 2)
			parent := prettify(parts[0])
			var subtestName string
			isSub := false
			if len(parts) == 2 {
				subtestName = prettify(parts[1])
				isSub = true
			}

			if _, ok := packages[event.Package].ParentMap[parent]; !ok {
				packages[event.Package].ParentMap[parent] = &ParentTest{Name: parent}
			}

			switch event.Action {
			case "pass":
				if isSub {
					packages[event.Package].ParentMap[parent].Subtests = append(
						packages[event.Package].ParentMap[parent].Subtests,
						SubTest{Name: subtestName, Passed: true, Time: event.Elapsed},
					)
					testsPassed++
				} else {
					packages[event.Package].ParentMap[parent].Passed = true
					testsPassed++
				}
				testsDone++
				printProgress(testsDone)
			case "fail":
				if isSub {
					packages[event.Package].ParentMap[parent].Subtests = append(
						packages[event.Package].ParentMap[parent].Subtests,
						SubTest{Name: subtestName, Passed: false, Time: event.Elapsed},
					)
					packages[event.Package].ParentMap[parent].Passed = false
					testsFailed++
				} else {
					packages[event.Package].ParentMap[parent].Passed = false
					testsFailed++
				}
				testsDone++
				printProgress(testsDone)
			}
		}

		// Detecta package pass/fail e duração
		if event.Action == "pass" && event.Test == "" {
			packages[event.Package].Passed = true
			packages[event.Package].Duration = event.Elapsed
		}
		if event.Action == "fail" && event.Test == "" {
			packages[event.Package].Passed = false
			packages[event.Package].Duration = event.Elapsed
		}
	}

	// Limpa a barra de progresso
	fmt.Print("\r" + strings.Repeat(" ", 60) + "\r")

	// Após ler tudo, ajusta SKIPPED e soma os resultados
	for _, pkg := range packages {
		if !pkg.HasTests {
			pkg.Skipped = true
			suitesSkipped++
		} else if pkg.Passed {
			suitesPassed++
		} else {
			suitesFailed++
		}
	}

	if *coverProfile == "" {
		// Print results agrupados
		for _, pkg := range packages {
			switch {
			case pkg.Skipped:
				color.New(color.FgYellow).Printf("%s  %s\n", color.New(color.Bold, color.BgYellow).Sprintf(" SKIP "), pkg.Name)
			case pkg.Passed:
				color.New(color.FgGreen).Printf(
					"%s  %s (%.2fs)\n",
					color.New(color.Bold, color.BgGreen).Sprintf(" PASS "),
					pkg.Name,
					pkg.Duration,
				)
			default:
				color.New(color.FgRed).Printf(
					"%s  %s (%.2fs)\n",
					color.New(color.Bold, color.BgRed).Sprintf(" FAIL "),
					pkg.Name,
					pkg.Duration,
				)
			}
			// Parent tests e subtests
			for _, pt := range pkg.ParentMap {
				if pt.Passed {
					color.New(color.FgGreen).Printf("  ✓ %s\n", pt.Name)
				} else {
					color.New(color.FgRed).Printf("  ✗ %s\n", pt.Name)
				}
				for _, st := range pt.Subtests {
					prefix := "     ✓"
					c := color.FgGreen
					if !st.Passed {
						prefix = "     ✗"
						c = color.FgRed
					}
					color.New(c).Printf("%s %s\n", prefix, st.Name)
				}
			}
			fmt.Println()
		}
	} else {
		for _, pkg := range packages {
			var tag string

			if pkg.Passed {
				tag = text.Colors{text.BgHiGreen, text.FgWhite, text.Bold}.Sprintf(" PASS ")
			}

			if pkg.Skipped {
				tag = text.Colors{text.BgYellow, text.FgWhite, text.Bold}.Sprintf(" SKIP ")
			}

			if !pkg.Passed && !pkg.Skipped {
				tag = text.Colors{text.BgRed, text.FgWhite, text.Bold}.Sprintf(" FAIL ")
			}

			fmt.Println(
				fmt.Sprintf(
					"%s   %s",
					tag,
					text.Colors{text.FgWhite, text.Bold}.Sprint(pkg.Name),
				),
			)
		}

		fmt.Println()
		fmt.Println()

		fileData, err := parseCoverProfile(*coverProfile)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		tree := buildTree(fileData)
		aggregate(tree)

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(
			table.Row{
				text.Bold.Sprint("File"),
				text.Bold.Sprint("% Coverage"),
				text.Bold.Sprint("% Lines"),
				text.Bold.Sprint("Uncovered Lines #s"),
			},
		)

		addRows(t, tree, 0)

		t.SetStyle(table.StyleRounded)
		t.Style().Options.SeparateRows = false
		t.Render()
	}

	fmt.Println()
	fmt.Println()

	// Summary
	fmt.Print(text.Bold.Sprintf("Test Suites: "))
	if suitesFailed > 0 {
		fmt.Print(text.Colors{text.FgRed, text.Bold}.Sprintf("%d failed, ", suitesFailed))
	}
	if suitesPassed > 0 {
		fmt.Print(text.Colors{text.FgHiGreen, text.Bold}.Sprintf("%d passed, ", suitesPassed))
	}
	if suitesSkipped > 0 {
		fmt.Print(text.Colors{text.FgCyan, text.Bold}.Sprintf("%d skipped, ", suitesSkipped))
	}
	fmt.Print(text.Bold.Sprintf("%d total\n", suitesPassed+suitesFailed+suitesSkipped))

	fmt.Print(text.Bold.Sprintf("Tests:       "))
	if testsFailed > 0 {
		fmt.Print(text.Colors{text.FgRed, text.Bold}.Sprintf("%d failed, ", testsFailed))
	}
	if testsPassed > 0 {
		fmt.Print(text.Colors{text.FgHiGreen, text.Bold}.Sprintf("%d passed, ", testsPassed))
	}
	fmt.Print(text.Bold.Sprintf("%d total\n", testsPassed+testsFailed))

	totalTime := time.Since(start).Seconds()
	fmt.Print(text.Bold.Sprintf("Time:        %.2fs\n", totalTime))
}
