package gen

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"
	"reflect"
	"strings"
	"text/template"
	"unicode"

	"github.com/fatih/structtag"
)

var buf *bytes.Buffer
var pkg string

func Diff(v reflect.Value, file string) error {
	t := v.Type()
	pkg = removePackagePrefix(t.PkgPath())
	buf = bytes.NewBuffer(nil)
	header(pkg)
	if err := genStruct(t, v); err != nil {
		return err
	}
	if err := genMergeMethod(t, v); err != nil {
		return err
	}
	if err := genCreateDiffMethod(t, v); err != nil {
		return err
	}
	if err := genCopyMethod(t, v); err != nil {
		return err
	}
	return save(file)
}

func ValueDiff(v reflect.Value) error {
	file := nonExported(v.Type().Name()) + "_diff.go"

	t := v.Type()
	pkg = removePackagePrefix(t.PkgPath())
	buf = bytes.NewBuffer(nil)
	header(pkg)
	if err := genStruct(t, v); err != nil {
		return err
	}
	if err := genValueMergeMethod(t, v); err != nil {
		return err
	}
	if err := genValueDiffMethod(t, v); err != nil {
		return err
	}
	return save(file)
}

func ValueCopyMerge(v reflect.Value, file string) error {
	t := v.Type()
	pkg = removePackagePrefix(t.PkgPath())
	buf = bytes.NewBuffer(nil)
	header(pkg)
	if err := genValueCopyMerge(t, v); err != nil {
		return err
	}
	return save(file)
}

// Client generira _client strukturu u kojoj su strukture koje imaju prijevode
// zamjenjene sa stringovima
func Client(v reflect.Value, file string) error {
	t := v.Type()
	pkg = removePackagePrefix(t.PkgPath())
	buf = bytes.NewBuffer(nil)
	header(pkg)
	if err := genClientStruct(t, v); err != nil {
		return err
	}
	return save(file)
}

// Client generira _client strukturu u kojoj su strukture koje imaju prijevode
// zamjenjene sa stringovima
func ClientDiff(v reflect.Value, file string) error {
	t := v.Type()
	pkg = removePackagePrefix(t.PkgPath())
	buf = bytes.NewBuffer(nil)
	header(pkg)
	if err := genMergeMethod(t, v); err != nil {
		return err
	}
	return save(file)
}

func JsKeys(v reflect.Value, file string) error {
	t := v.Type()
	pkg = removePackagePrefix(t.PkgPath())
	buf = bytes.NewBuffer(nil)
	w(`// Code generated by go generate; DO NOT EDIT.
`)

	w(`function unpack(o) {`)
	w(`
  function unpackObject(o, keys) {
    for (var short in keys) {
      var long = keys[short];
      if (o[short] !== undefined) {
        o[long] = o[short];
        delete o[short];
      }
    }
  }

  function unpackMap(map, elementUnpacker) {
    if (map === undefined) {
      return;
    }
    map["_isMap"] = true;
    for(var k in map) {
      var c = map[k];
      if (c !== null) {
        elementUnpacker(c);
      }
    }
  }`)
	root, err := genJsKeys(t, v)
	if err != nil {
		return err
	}
	w(`
  _%s(o);
  return o;
}
`, root)
	w(`module.exports = unpack;`)
	return ioutil.WriteFile(file, buf.Bytes(), 0644)
}

func AmpInterface(v reflect.Value, file string) error {
	t := v.Type()
	buf = bytes.NewBuffer(nil)
	header(removePackagePrefix(t.PkgPath()))

	d := getTemplateData(t, v, "", nil)
	if err := ampTemplate.Execute(buf, d); err != nil {
		return err
	}

	return save(file)
}

func header(pkg string) {
	w(`// Code generated by go generate; DO NOT EDIT.
package %s

`, pkg)
}

// generates same struct as t with all nilable (pointer) types
func genStruct(t reflect.Type, v reflect.Value) error {
	return runTemplate(diffStructTemplate, t, v, "")
}

func genJsKeys(t reflect.Type, v reflect.Value) (string, error) {
	return runTemplateParent(jsKeysTemplate, t, v, "", nil)
}

func genClientStruct(t reflect.Type, v reflect.Value) error {
	return runTemplate(langStructTemplate, t, v, "")
}

func genMergeMethod(t reflect.Type, v reflect.Value) error {
	return runTemplate(mergeMethodTemplate, t, v, "")
}

func genValueMergeMethod(t reflect.Type, v reflect.Value) error {
	return runTemplate(valueMergeMethodTemplate, t, v, "")
}

func genCreateDiffMethod(t reflect.Type, v reflect.Value) error {
	return runTemplate(createDiffTemplate, t, v, "")
}

func genValueDiffMethod(t reflect.Type, v reflect.Value) error {
	return runTemplate(valueDiffTemplate, t, v, "")
}

func genAdapterDiffMethod(t reflect.Type, v reflect.Value) error {
	return runTemplate(adapterDiffTemplate, t, v, "adp")
}

func genCopyMethod(t reflect.Type, v reflect.Value) error {
	return runTemplate(copyMethodTemplate, t, v, "")
}

func genValueCopyMerge(t reflect.Type, v reflect.Value) error {
	return runTemplate(valueCopyMergeTemplate, t, v, "")
}

func w(format string, a ...interface{}) {
	buf.WriteString(fmt.Sprintf(format, a...))
	buf.WriteString("\n")
}

func runTemplate(tpl *template.Template, t reflect.Type, v reflect.Value, tag string) error {
	_, err := runTemplateParent(tpl, t, v, tag, nil)
	return err
}

func runTemplateParent(tpl *template.Template, t reflect.Type, v reflect.Value, tag string, pt reflect.Type) (string, error) {
	d := getTemplateData(t, v, tag, pt)
	if err := tpl.Execute(buf, d); err != nil {
		return "", err
	}
	for _, m := range d.Maps {
		// osiguranje da za jedan tip samo jednom vrtimo template (kada razliciti struct-ovi imaju iste childs)
		key := fmt.Sprintf("%v-%s-%s-%s", tpl, m.ValueType.Name(), m.ValueValue.String(), tag)
		if _, ok := done[key]; ok {
			continue
		}
		if _, err := runTemplateParent(tpl, m.ValueType, m.ValueValue, tag, t); err != nil {
			return "", err
		}
		done[key] = struct{}{}
	}
	for _, s := range d.Structs {
		if _, err := runTemplateParent(tpl, s.FieldType, s.FieldValue, tag, t); err != nil {
			return "", err
		}
	}
	return d.Type, nil
}

var done = make(map[string]struct{})

func findFields(t reflect.Type, v reflect.Value) ([]int, []int, map[int]bool, map[int]bool, bool, []int, map[int]bool) {
	nt := reflect.New(t)

	hasMethod := func(name string) bool {
		if v := nt.MethodByName(name); v.IsValid() {
			return true
		}
		if _, ok := t.MethodByName(name); ok {
			return true
		}
		return false
	}

	var exported []int
	var maps []int
	var structs []int
	isStruct := make(map[int]bool)
	isSlice := make(map[int]bool)
	hasChangedMethod := make(map[int]bool)
	equalType := reflect.TypeOf((*equalInterface)(nil)).Elem()
	for i := 0; i < v.NumField(); i++ {
		hasChangedMethod[i] = false
		isStruct[i] = false
		isSlice[i] = false
		f := v.Field(i)
		if !f.CanSet() { // skip unexported
			continue
		}
		ft := t.Field(i)
		switch ft.Type.Kind() {
		case reflect.Map:
			maps = append(maps, i)
			continue
		case reflect.Invalid, reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
			// skip this types
			continue
		case reflect.Array, reflect.Slice:
			isSlice[i] = true
		case reflect.Struct:
			ts := ft.Type.String()
			if ts == "sync.Mutex" {
				continue
			}
			if ts != "time.Time" {
				if ft.Type.Implements(equalType) {
					isStruct[i] = true
				} else {
					structs = append(structs, i)
					// skiping struct type wich does not have Equal method
					continue
				}
			}
		}
		mn := fmt.Sprintf("%sChanged", ft.Name)
		hasChangedMethod[i] = hasMethod(mn)
		exported = append(exported, i)
	}
	return exported, maps, isStruct, hasChangedMethod, hasMethod("Ts"), structs, isSlice
}

type equalInterface interface {
	Equal(interface{}) bool
}

type translatorInterface interface {
	Lang(string) string
}

func save(file string) error {
	err := ioutil.WriteFile(file, buf.Bytes(), 0644)
	if err != nil {
		return err
	}
	err = exec.Command("go", "fmt", file).Run()
	if err != nil {
		return fmt.Errorf("go fmt failed with error: %s", err)
	}
	err = exec.Command("goimports", "-w", file).Run()
	if err != nil {
		return fmt.Errorf("go imports failed with error: %s", err)
	}
	fmt.Printf("generated %s\n", file)
	return nil
}

// nonExported name; first letter lower
func nonExported(s string) string {
	a := []rune(s)
	a[0] = unicode.ToLower(a[0])
	return string(a)
}

func removePackagePrefix(typ string) string {
	p := strings.Split(typ, "/")
	return p[len(p)-1]
}

type templateData struct {
	Type             string
	TypeLower        string
	Fields           []fieldData
	Maps             []mapData
	Structs          []structData
	EmptyParts       []string
	HasChangedMethod bool
	HasTsMethod      bool
	HasParent        bool
	ParentName       string
	ParentType       string
}

type fieldData struct {
	Name             string
	Type             string
	ClientType       string
	IsClientType     bool
	NameLower        string
	Tag              string
	HasChangedMethod bool
	IsStruct         bool
	IsSlice          bool
	TagName          string
}

func (d fieldData) NeedUnpack() bool {
	return d.TagName != d.NameLower
}

type mapData struct {
	Field         string
	FieldLower    string
	Key           string
	Value         string
	KeyType       reflect.Type
	ValueType     reflect.Type
	ValueValue    reflect.Value
	HasHiddenAttr bool
	HasParent     bool
	HasKeyAttr    bool
	ParentAttr    string
	Tag           string
	TagName       string
}

func (d mapData) NeedUnpack() bool {
	return d.TagName != d.FieldLower
}

type structData struct {
	Field      string
	FieldLower string
	Type       string
	FieldType  reflect.Type
	FieldValue reflect.Value
	HasParent  bool
	HasKeyAttr bool
	ParentAttr string
	Tag        string
	TagName    string
}

func (d structData) NeedUnpack() bool {
	return d.TagName != d.FieldLower
}

func parseTag(tag string, jn string, tagName string) (bool, string, string) {
	tags, err := structtag.Parse(string(tag))
	if err != nil {
		panic(err)
	}
	if t, err := tags.Get(tagName); err == nil && t.Name == "-" {
		return true, "", ""
	}
	if t, err := tags.Get("json"); err == nil {
		jn = t.Name
	}
	mn := jn
	if t, err := tags.Get("msg"); err == nil {
		mn = t.Name
	}
	return false, "`" + `json:"` + jn + `,omitempty" bson:"` + jn + `,omitempty" msg:"` + mn + `"` + "`", jn
}

func parentName(t reflect.Type) string {
	n := nonExported(t.Name())
	n = strings.TrimSuffix(n, "Diff")
	n = strings.TrimSuffix(n, "Client")
	return n
}

func getTemplateData(t reflect.Type, v reflect.Value, tagName string,
	pt reflect.Type) templateData {
	translatorType := reflect.TypeOf((*translatorInterface)(nil)).Elem()
	exported, maps, isStruct, hasChangedMethod, hasTsMethod, structs, isSlice := findFields(t, v)
	d := templateData{
		Type:             t.Name(),
		TypeLower:        nonExported(t.Name()),
		HasChangedMethod: hasMethod(t, "Changed"),
		HasTsMethod:      hasTsMethod,
	}
	if pt != nil {
		d.HasParent = true
		d.ParentName = parentName(pt)
		d.ParentType = pt.Name()
	}
	for _, i := range exported {
		f := v.Field(i)
		ft := t.Field(i)
		nl := nonExported(ft.Name)

		skip, tag, jn := parseTag(string(ft.Tag), nl, tagName)
		if skip {
			continue
		}
		typ := strings.TrimPrefix(f.Type().String(), pkg+".")
		ltyp := typ
		if f.Type().Implements(translatorType) {
			ltyp = "string"
		}
		d.Fields = append(d.Fields, fieldData{
			Name:             ft.Name,
			Type:             typ,
			ClientType:       ltyp,
			IsClientType:     typ != ltyp,
			NameLower:        nl,
			HasChangedMethod: hasChangedMethod[i],
			IsStruct:         isStruct[i],
			IsSlice:          isSlice[i],
			Tag:              tag,
			TagName:          jn,
		})
		p := fmt.Sprintf(" o.%s == nil ", ft.Name)
		d.EmptyParts = append(d.EmptyParts, p)
	}
	for _, i := range maps {
		ft := t.Field(i)
		et := ft.Type.Elem() // map value
		if et.Kind() == reflect.Ptr {
			et = et.Elem()
		}
		kt := ft.Type.Key() // map key
		_, hasHiddenAttr := et.FieldByName("hidden")
		pn := parentName(t)
		_, hasParent := et.FieldByName(pn)
		_, hasKeyAttr := et.FieldByName("key")
		nl := nonExported(ft.Name)
		if !hasParent {
			pn = ""
		}
		skip, tag, jn := parseTag(string(ft.Tag), nl, tagName)
		if skip {
			continue
		}
		d.Maps = append(d.Maps, mapData{
			Field:         ft.Name,
			FieldLower:    nl,
			Key:           kt.Name(),
			Value:         et.Name(),
			ValueType:     et,
			KeyType:       kt,
			ValueValue:    reflect.New(et).Elem(),
			HasHiddenAttr: hasHiddenAttr,
			HasParent:     hasParent,
			HasKeyAttr:    hasKeyAttr,
			ParentAttr:    pn,
			Tag:           tag,
			TagName:       jn,
		})
		p := fmt.Sprintf(" (o.%s == nil || len(o.%s) == 0) ", ft.Name, ft.Name)
		d.EmptyParts = append(d.EmptyParts, p)
	}
	for _, i := range structs {
		ft := t.Field(i)
		et := ft.Type // map value
		//_, hasHiddenAttr := et.FieldByName("hidden")
		pn := parentName(t)
		_, hasParent := et.FieldByName(pn)
		_, hasKeyAttr := et.FieldByName("key")
		nl := nonExported(ft.Name)
		if !hasParent {
			pn = ""
		}
		skip, tag, jn := parseTag(string(ft.Tag), nl, tagName)
		if skip {
			continue
		}
		//typ := strings.TrimPrefix(et.String(), pkg+".")
		d.Structs = append(d.Structs, structData{
			Field:      ft.Name,
			FieldLower: nl,
			Type:       et.Name(),
			FieldType:  et,
			FieldValue: reflect.New(et).Elem(),
			HasParent:  hasParent,
			HasKeyAttr: hasKeyAttr,
			ParentAttr: pn,
			Tag:        tag,
			TagName:    jn,
		})
		p := fmt.Sprintf(" o.%s == nil ", ft.Name)
		d.EmptyParts = append(d.EmptyParts, p)
	}
	return d
}

func hasMethod(t reflect.Type, name string) bool {
	nt := reflect.New(t)
	if v := nt.MethodByName(name); v.IsValid() {
		return true
	}
	if _, ok := t.MethodByName(name); ok {
		return true
	}
	return false
}
