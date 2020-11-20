package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"github.com/goccy/go-graphviz/cgraph"
	"io/ioutil"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-graphviz"
)

type Cmp struct {
	Name		string
	IsExpose	bool
	SystemName	string
	Path		string
	Html		[]string
	Js			[]string
	Use			[]string
	FlexiPages	[]string
}

type FlexiPage struct {
	Path	string
	Name	string
}

type LightningComponentBundle struct {
	XMLName		xml.Name	`xml:"LightningComponentBundle"`
	IsExposed	bool		`xml:"isExposed"`
}

func main() {

	graphLayer := flag.String("l", "dot", "layer for graphviz\nSupport: circo, dot, fdp, neato, nop, nop1, nop2, osage, patchwork, sfdp, twopi\n")
	flag.Parse()

	projectDir := "/Users/mmorozov/Rpaas/mm2-rpaas"
	lwcDir := projectDir + "/force-app/main/default/lwc"
	flexiPagesDir := projectDir + "/force-app/main/default/flexipages"

	_, err := os.Stat(lwcDir)
	if os.IsNotExist(err) {
		fmt.Printf("%s dir is not exists\n", lwcDir)
		return
	}

	flexiPages := []*FlexiPage{}
	_, err = os.Stat(flexiPagesDir)
	if !os.IsNotExist(err) {
		flexiPages, _ = ReadFlexiPages(flexiPagesDir)
	}

	cmpList := []*Cmp{}
	cmpList = ReadComponents(lwcDir, cmpList)

	sort.Slice(cmpList, func(i, j int) bool { return cmpList[i].Name < cmpList[j].Name })

	fmt.Println("LWC Components amount ", len(cmpList))

	//Search dependencies
	for _, cmp := range cmpList {

		reName := regexp.MustCompile(cmp.Name)
		reSystemName := regexp.MustCompile("\\<" + strings.ReplaceAll(cmp.SystemName, "-", "\\-") + "\\s")
		reImportJs := regexp.MustCompile("from\\s*[\\\"']c/" + cmp.Name + "[\"']\\s*\\;")

		for _, e := range cmpList {
			for _, f := range e.Html {
				content, err := ioutil.ReadFile(e.Path + "/" + f)

				if err != nil {
					fmt.Printf("Cannot open %s", e.Path + "/" + f)
				}

				if reSystemName.Match(content) {
					cmp.Use = append(cmp.Use, e.Name)
				}
			}

			for _, f := range e.Js {

				content, err := ioutil.ReadFile(e.Path + "/" + f)

				if err != nil {
					fmt.Printf("Cannot open %s", e.Path + "/" + f)
				}

				if reImportJs.Match(content) {
					cmp.Use = append(cmp.Use, e.Name)
				}
			}
		}

		if cmp.IsExpose && len(flexiPages) > 0 {
			for _, f := range flexiPages {
				content, err := ioutil.ReadFile(f.Path)

				if err != nil {
					fmt.Printf("Cannot read %s\n", f.Path)
					continue
				}

				if reName.Match(content) {
					cmp.FlexiPages = append(cmp.FlexiPages, f.Name)
				}
			}
		}

		if false {
			fmt.Println(cmp.Name)
			fmt.Printf("\tIs Expose:\t%t\n", cmp.IsExpose)
			fmt.Printf("\tSystem Name:\t%s\n", cmp.SystemName)
			fmt.Printf("\tPath:\t\t%s\n", cmp.Path)
			fmt.Printf("\tHTML:\n")
			for _, s := range cmp.Html {
				fmt.Printf("\t    - %s\n", s)
			}
			fmt.Printf("\tJS:\n")
			for _, s := range cmp.Js {
				fmt.Printf("\t    - %s\n", s)
			}
			fmt.Printf("\tUsage:\n")
			for _, s := range cmp.Use {
				fmt.Printf("\t    - %s\n", s)
			}
			fmt.Printf("\tFlexiPages:\n")
			for _, s := range cmp.FlexiPages {
				fmt.Printf("\t    - %s\n", s)
			}
			fmt.Println("----------------")
		}
	}

	GenerateGraph(cmpList, flexiPages, *graphLayer)
}

func ReadComponents(dirname string, cmpList []*Cmp) []*Cmp {

	listOfFiles, err := ReadDir(dirname)

	if err != nil {
		return cmpList
	}

	for _, f := range listOfFiles {
		if f.IsDir() {
			cmpPath := dirname + "/" + f.Name()
			metaFile := cmpPath + "/" + f.Name() + ".js-meta.xml"
			_, err := os.Stat(metaFile)

			if os.IsNotExist(err) {
				cmpList = ReadComponents(cmpPath, cmpList)
			} else {
				cmp, err := createComponent(cmpPath, f.Name())

				if err != nil {
					continue
				}

				cmpList = append(cmpList, cmp)
			}
		}
	}

	return cmpList
}

func ReadFlexiPages(path string) ([]*FlexiPage, error) {
	listOfFiles, err := ReadDir(path)

	if err != nil {
		fmt.Printf("Cannot read %s\n", path)
		return []*FlexiPage{}, err
	}

	files := []*FlexiPage{}

	for _, f := range listOfFiles {
		files = append(files, &FlexiPage{
			Path: path + "/" + f.Name(),
			Name: strings.ReplaceAll(f.Name(), ".flexipage-meta.xml", ""),
		})
	}

	return files, nil
}

func createComponent(path string, name string) (*Cmp, error) {
	cmp := Cmp{
		Name: name,
		Path: path,
	}

	metaFile := path + "/" + name + ".js-meta.xml"
	metaData, err := ioutil.ReadFile(metaFile)

	if err != nil {
		fmt.Printf("Cannot read meta file %s", metaFile)
		return nil, err
	}

	//Read meta file
	bundle := LightningComponentBundle{}
	err = xml.Unmarshal(metaData, &bundle)

	if err != nil {
		fmt.Printf("Cannot parse meta file %s", metaFile)
		return nil, err
	}

	cmp.IsExpose = bundle.IsExposed

	//Generate system name
	re := regexp.MustCompile(`[A-Za-z][a-z]+`)
	parts := []string{}

	for _, match := range re.FindAllString(name, -1) {
		parts = append(parts, strings.ToLower(match))
	}

	cmp.SystemName = "c-" + strings.Join(parts, "-")

	//Collect html and js files
	listOfFiles, err := ReadDir(path)

	if err != nil {
		fmt.Printf("Cannot read path %s", path)
		return nil, err
	}

	reJs := regexp.MustCompile(`.*\.js$`)
	reHtml := regexp.MustCompile(`.*\.html$`)

	html := []string{}
	js := []string{}

	for _, f := range listOfFiles {
		if !f.IsDir() {
			for _, match := range reHtml.FindAllString(f.Name(), -1) {
				html = append(html, match)
			}
			for _, match := range reJs.FindAllString(f.Name(), -1) {
				js = append(js, match)
			}
		}
	}

	cmp.Html = html
	cmp.Js = js

	return &cmp, nil
}

func ReadDir(dirname string) ([]os.FileInfo, error) {
	f, err := os.Open(dirname)
	if err != nil {
		fmt.Printf("Cannot open %s\n", dirname)
		return nil, err
	}
	list, err := f.Readdir(-1)
	f.Close()

	if err != nil {
		fmt.Printf("Cannot read %s\n", dirname)
		return nil, err
	}

	return list, nil
}

func GenerateGraph(cmpList []*Cmp, flexiPages []*FlexiPage, layer string) {

	fmt.Println("Start build graph")
	fmt.Printf("    Layout: %s\n", layer)

	tStart := time.Now()

	g := graphviz.New()
	graph, err := g.Graph()

	if err != nil {
		fmt.Println("Cannot create graph")
		return
	}
	graph.SetLayout(layer)

	mapNodes := make(map[string]*cgraph.Node)

	//Creating all nodes
	for _, cmp := range cmpList {
		mapNodes[cmp.Name], _ = graph.CreateNode(cmp.Name)
	}
	for _, p := range flexiPages {
		e, _ := graph.CreateNode(p.Name)
		e.SetFillColor("#3d8bff")
		e.SetColor("#3d8bff")
		e.SetFontColor("#3d8bff")
		mapNodes[p.Name] = e
	}

	for _, cmp := range cmpList {
		if len(cmp.Use) > 0 {
			for _,c := range cmp.Use {
				graph.CreateEdge(cmp.Name + " > " + c, mapNodes[cmp.Name], mapNodes[c])
			}
		}
		if len(cmp.FlexiPages) > 0 {
			for _,c := range cmp.FlexiPages {
				graph.CreateEdge(cmp.Name + " > " + c, mapNodes[cmp.Name], mapNodes[c])
			}
		}
	}

	if err := g.RenderFilename(graph, graphviz.SVG, "./graph.svg"); err != nil {
		fmt.Println("Cannot write image file")
	}

	tEnd := time.Now()
	fmt.Printf("\tBuild time: %v\n", tEnd.Sub(tStart))
}