package deepcopy

import (
	"testing"

	json "github.com/mohae/customjson"
)

// This tests both []interface{} that contains strings and []string
// along with an interface containing a single string
func TestInterfaceToSliceOfStrings(t *testing.T) {
	tests := []struct {
		value    []interface{}
		expected []string
	}{
		{nil, []string{}},
		{[]interface{}{}, []string{}},
		{[]interface{}{"A", "B", "C"}, []string{"A", "B", "C"}},
		{[]interface{}{"A", "B", "B"}, []string{"A", "B", "B"}},
	}
	for i, test := range tests {
		cpy := InterfaceToSliceOfStrings(test.value)
		for j, v := range cpy {
			if v != test.expected[j] {
				t.Errorf("%d: expected copy to be %#v, got %#v", i, test.expected, cpy)
				break
			}
		}
	}
	tests2 := []struct {
		value    []string
		expected []string
	}{
		{nil, []string{}},
		{[]string{}, []string{}},
		{[]string{"A", "B", "C"}, []string{"A", "B", "C"}},
		{[]string{"A", "B", "B"}, []string{"A", "B", "B"}},
	}
	for i, test := range tests2 {
		cpy := InterfaceToSliceOfStrings(test.value)
		for j, v := range cpy {
			if v != test.expected[j] {
				t.Errorf("%d: expected copy to be %#v, got %#v", i, test.expected, cpy)
				break
			}
		}
	}
	cpy := InterfaceToSliceOfStrings("ABC")
	if len(cpy) != 1 {
		t.Errorf("Expected a non-slice value to result in a slice length of 1, got %d", len(cpy))
	} else {
		if cpy[0] != "ABC" {
			t.Errorf("Expected a non-slice string to return a string slice with \"ABC\", got %s", cpy[0])
		}
	}
}

// This tests both []interface{} that contains ints and []int
// along with an interface containing a single int
func TestInterfaceToSliceOfInt(t *testing.T) {
	tests := []struct {
		value    []interface{}
		expected []int
	}{
		{nil, []int{}},
		{[]interface{}{}, []int{}},
		{[]interface{}{1, 2, 3}, []int{1, 2, 3}},
		{[]interface{}{1, 2, 2}, []int{1, 2, 2}},
	}
	for i, test := range tests {
		cpy := InterfaceToSliceOfInts(test.value)
		for j, v := range cpy {
			if v != test.expected[j] {
				t.Errorf("%d: expected copy to be %#v, got %#v", i, test.expected, cpy)
				break
			}
		}
	}
	tests2 := []struct {
		value    []int
		expected []int
	}{
		{nil, []int{}},
		{[]int{}, []int{}},
		{[]int{1, 2, 3}, []int{1, 2, 3}},
		{[]int{1, 2, 2}, []int{1, 2, 2}},
	}
	for i, test := range tests2 {
		cpy := InterfaceToSliceOfInts(test.value)
		for j, v := range cpy {
			if v != test.expected[j] {
				t.Errorf("%d: expected copy to be %#v, got %#v", i, test.expected, cpy)
				break
			}
		}
	}
	cpy := InterfaceToSliceOfInts(1)
	if len(cpy) != 1 {
		t.Errorf("Expected a non-slice value to result in a slice length of 1, got %d", len(cpy))
	} else {
		if cpy[0] != 1 {
			t.Errorf("Expected a non-slice string to return a string slice with 1, got %d", cpy[0])
		}
	}
}

// This tests both []interface{} and interface{}
func TestIface(t *testing.T) {
	tests := []struct {
		value    interface{}
		expected interface{}
	}{
		{nil, nil},
		{[]int{1, 2, 2}, []int{1, 2, 2}},
		{[]int8{1, 2, 2}, []int8{1, 2, 2}},
		{[]int16{1, 2, 2}, []int16{1, 2, 2}},
		{[]int32{1, 2, 2}, []int32{1, 2, 2}},
		{[]int64{1, 2, 2}, []int64{1, 2, 2}},
		{[]float32{1.1, 2.2, 3.154}, []float32{1.1, 2.2, 3.154}},
		{[]float64{1.1, 2.2, 3.154}, []float64{1.1, 2.2, 3.154}},
		{[]string{"A", "B", "C"}, []string{"A", "B", "C"}},
		{[]bool{true, true, false, true, false}, []bool{true, true, false, true, false}},
		{[]interface{}{"A", "B", "C", 1, 2, 3}, []interface{}{"A", "B", "C", 1, 2, 3}},
		{map[string]string{"A": "AA", "B": "BB", "C": "CC"}, map[string]string{"A": "AA", "B": "BB", "C": "CC"}},
		{map[int]int{1: 100, 2: 200, 3: 300}, map[int]int{1: 100, 2: 200, 3: 300}},
		{map[string]int{"A": 1, "B": 2, "C": 3, "D": 4}, map[string]int{"A": 1, "B": 2, "C": 3, "D": 4}},
		{map[int]string{1: "A", 2: "B", 3: "C"}, map[int]string{1: "A", 2: "B", 3: "C"}},
		{map[string]interface{}{"A": "a", "B": "b", "C": 100, "D": 4.4}, map[string]interface{}{"A": "a", "B": "b", "C": 100, "D": 4.4}},
		{map[string][]interface{}{"A": []interface{}{"a", 1, 1.2}, "B": []interface{}{"b", 2, 2.2}}, map[string][]interface{}{"A": []interface{}{"a", 1, 1.2}, "B": []interface{}{"b", 2, 2.2}}},
		{map[string]bool{"a": true, "b": false, "c": false, "d": true}, map[string]bool{"a": true, "b": false, "c": false, "d": true}},
	}
	for i, test := range tests {
		cpy := Iface(test.value)
		if json.MarshalToString(cpy) != json.MarshalToString(test.expected) {
			t.Errorf("%d: expected copy to be %#v, got %#v", i, test.expected, cpy)
		}
	}
}

// TestStruct has both exported and unexported fields for testing can set.
type TestStruct struct {
	Strings string
	strings string
	Ints    int
	ints    int
	SSlice  []string
	sSlice  []string
	ISlice  []int
	iSlice  []int
	SSMap   map[string]string
	sSMap   map[string]string
}

func TestCanSet(t *testing.T) {
	tst := TestStruct{
		Strings: "an exported string",
		strings: "an unexported string",
		Ints:    42,
		ints:    11,
		SSlice:  []string{"hello", "world"},
		sSlice:  []string{"don't", "panic"},
		ISlice:  []int{1, 2, 3},
		iSlice:  []int{42, 11},
		SSMap:   map[string]string{"french": "bonjour", "spanish": "hola"},
		sSMap:   map[string]string{"french": "au revoir", "spanish": "adios"},
	}
	expected := TestStruct{
		Strings: "an exported string",
		Ints:    42,
		SSlice:  []string{"hello", "world"},
		ISlice:  []int{1, 2, 3},
		SSMap:   map[string]string{"french": "bonjour", "spanish": "hola"},
	}
	cpy := Iface(tst)
	if json.MarshalToString(cpy) != json.MarshalToString(expected) {
		t.Errorf("Expected copy to be %#v, got %#v\n", expected, cpy)
	}
}
