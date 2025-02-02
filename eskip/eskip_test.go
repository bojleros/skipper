package eskip

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sanity-io/litter"
)

func checkItems(t *testing.T, message string, l, lenExpected int, checkItem func(int) bool) bool {
	if l != lenExpected {
		t.Error(message, "length", l, lenExpected)
		return false
	}

	for i := 0; i < l; i++ {
		if !checkItem(i) {
			t.Error(message, "item", i)
			return false
		}
	}

	return true
}

func checkFilters(t *testing.T, message string, fs, fsExp []*Filter) bool {
	return checkItems(t, "filters "+message,
		len(fs),
		len(fsExp),
		func(i int) bool {
			return fs[i].Name == fsExp[i].Name &&
				checkItems(t, "filter args",
					len(fs[i].Args),
					len(fsExp[i].Args),
					func(j int) bool {
						return fs[i].Args[j] == fsExp[i].Args[j]
					})
		})
}

func TestParseRouteExpression(t *testing.T) {
	for _, ti := range []struct {
		msg        string
		expression string
		check      *Route
		err        bool
	}{{
		"loadbalancer endpoints same protocol",
		`* -> <roundRobin, "http://localhost:80", "fastcgi://localhost:80">`,
		nil,
		true,
	}, {
		"path predicate",
		`Path("/some/path") -> "https://www.example.org"`,
		&Route{Path: "/some/path", Backend: "https://www.example.org"},
		false,
	}, {
		"path regexp",
		`PathRegexp("^/some") && PathRegexp(/\/\w+Id$/) -> "https://www.example.org"`,
		&Route{
			PathRegexps: []string{"^/some", "/\\w+Id$"},
			Backend:     "https://www.example.org"},
		false,
	}, {
		"weight predicate",
		`Weight(50) -> "https://www.example.org"`,
		&Route{
			Predicates: []*Predicate{
				{"Weight", []interface{}{float64(50)}},
			},
			Backend: "https://www.example.org",
		},
		false,
	}, {
		"method predicate",
		`Method("HEAD") -> "https://www.example.org"`,
		&Route{Method: "HEAD", Backend: "https://www.example.org"},
		false,
	}, {
		"invalid method predicate",
		`Path("/endpoint") && Method("GET", "POST") -> "https://www.example.org"`,
		nil,
		true,
	}, {
		"host regexps",
		`Host(/^www[.]/) && Host(/[.]org$/) -> "https://www.example.org"`,
		&Route{HostRegexps: []string{"^www[.]", "[.]org$"}, Backend: "https://www.example.org"},
		false,
	}, {
		"headers",
		`Header("Header-0", "value-0") &&
		Header("Header-1", "value-1") ->
		"https://www.example.org"`,
		&Route{
			Headers: map[string]string{"Header-0": "value-0", "Header-1": "value-1"},
			Backend: "https://www.example.org"},
		false,
	}, {
		"header regexps",
		`HeaderRegexp("Header-0", "value-0") &&
		HeaderRegexp("Header-0", "value-1") &&
		HeaderRegexp("Header-1", "value-2") &&
		HeaderRegexp("Header-1", "value-3") ->
		"https://www.example.org"`,
		&Route{
			HeaderRegexps: map[string][]string{
				"Header-0": {"value-0", "value-1"},
				"Header-1": {"value-2", "value-3"}},
			Backend: "https://www.example.org"},
		false,
	}, {
		"comment as last token",
		"route: Any() -> <shunt>; // some comment",
		&Route{Id: "route", BackendType: ShuntBackend, Shunt: true},
		false,
	}, {
		"catch all",
		`* -> "https://www.example.org"`,
		&Route{Backend: "https://www.example.org"},
		false,
	}, {
		"custom predicate",
		`Custom1(3.14, "test value") && Custom2() -> "https://www.example.org"`,
		&Route{
			Predicates: []*Predicate{
				{"Custom1", []interface{}{float64(3.14), "test value"}},
				{"Custom2", nil}},
			Backend: "https://www.example.org"},
		false,
	}, {
		"double path predicates",
		`Path("/one") && Path("/two") -> "https://www.example.org"`,
		nil,
		true,
	}, {
		"double method predicates",
		`Method("HEAD") && Method("GET") -> "https://www.example.org"`,
		nil,
		true,
	}, {
		"shunt",
		`* -> setRequestHeader("X-Foo", "bar") -> <shunt>`,
		&Route{
			Filters: []*Filter{
				{Name: "setRequestHeader", Args: []interface{}{"X-Foo", "bar"}},
			},
			BackendType: ShuntBackend,
			Shunt:       true,
		},
		false,
	}, {
		"loopback",
		`* -> setRequestHeader("X-Foo", "bar") -> <loopback>`,
		&Route{
			Filters: []*Filter{
				{Name: "setRequestHeader", Args: []interface{}{"X-Foo", "bar"}},
			},
			BackendType: LoopBackend,
		},
		false,
	}, {
		"dynamic",
		`* -> setRequestHeader("X-Foo", "bar") -> <dynamic>`,
		&Route{
			Filters: []*Filter{
				{Name: "setRequestHeader", Args: []interface{}{"X-Foo", "bar"}},
			},
			BackendType: DynamicBackend,
		},
		false,
	}} {
		t.Run(ti.msg, func(t *testing.T) {
			stringMapKeys := func(m map[string]string) []string {
				keys := make([]string, 0, len(m))
				for k := range m {
					keys = append(keys, k)
				}

				return keys
			}

			stringsMapKeys := func(m map[string][]string) []string {
				keys := make([]string, 0, len(m))
				for k := range m {
					keys = append(keys, k)
				}

				return keys
			}

			checkItemsT := func(submessage string, l, lExp int, checkItem func(i int) bool) bool {
				return checkItems(t, submessage, l, lExp, checkItem)
			}

			checkStrings := func(submessage string, s, sExp []string) bool {
				return checkItemsT(submessage, len(s), len(sExp), func(i int) bool {
					return s[i] == sExp[i]
				})
			}

			checkStringMap := func(submessage string, m, mExp map[string]string) bool {
				keys := stringMapKeys(m)
				return checkItemsT(submessage, len(m), len(mExp), func(i int) bool {
					return m[keys[i]] == mExp[keys[i]]
				})
			}

			checkStringsMap := func(submessage string, m, mExp map[string][]string) bool {
				keys := stringsMapKeys(m)
				return checkItemsT(submessage, len(m), len(mExp), func(i int) bool {
					return checkItemsT(submessage, len(m[keys[i]]), len(mExp[keys[i]]), func(j int) bool {
						return m[keys[i]][j] == mExp[keys[i]][j]
					})
				})
			}

			routes, err := Parse(ti.expression)
			if err == nil && ti.err || err != nil && !ti.err {
				t.Error("failure case", err, ti.err)
				return
			}

			if ti.err {
				return
			}

			r := routes[0]

			if r.Id != ti.check.Id {
				t.Error("id", r.Id, ti.check.Id)
				return
			}

			if r.Path != ti.check.Path {
				t.Error("path", r.Path, ti.check.Path)
				return
			}

			if !checkStrings("host", r.HostRegexps, ti.check.HostRegexps) {
				return
			}

			if !checkStrings("path regexp", r.PathRegexps, ti.check.PathRegexps) {
				return
			}

			if r.Method != ti.check.Method {
				t.Error("method", r.Method, ti.check.Method)
				return
			}

			if !checkStringMap("headers", r.Headers, ti.check.Headers) {
				return
			}

			if !checkStringsMap("header regexps", r.HeaderRegexps, ti.check.HeaderRegexps) {
				return
			}

			if !checkItemsT("custom predicates",
				len(r.Predicates),
				len(ti.check.Predicates),
				func(i int) bool {
					return r.Predicates[i].Name == ti.check.Predicates[i].Name &&
						checkItemsT("custom predicate args",
							len(r.Predicates[i].Args),
							len(ti.check.Predicates[i].Args),
							func(j int) bool {
								return r.Predicates[i].Args[j] == ti.check.Predicates[i].Args[j]
							})
				}) {
				return
			}

			if !checkFilters(t, "", r.Filters, ti.check.Filters) {
				return
			}

			if r.BackendType != ti.check.BackendType {
				t.Error("invalid backend type", r.BackendType, ti.check.BackendType)
			}

			if r.Shunt != ti.check.Shunt {
				t.Error("shunt", r.Shunt, ti.check.Shunt)
			}

			if r.Shunt && r.BackendType != ShuntBackend || !r.Shunt && r.BackendType == ShuntBackend {
				t.Error("shunt, deprecated and new form are not sync")
			}

			if r.BackendType == LoopBackend && r.Shunt {
				t.Error("shunt set for loopback route")
			}

			if r.Backend != ti.check.Backend {
				t.Error("backend", r.Backend, ti.check.Backend)
			}
		})
	}
}

func TestParseFilters(t *testing.T) {
	for _, ti := range []struct {
		msg        string
		expression string
		check      []*Filter
		err        bool
	}{{
		"empty",
		" \t",
		nil,
		false,
	}, {
		"error",
		"trallala",
		nil,
		true,
	}, {
		"success",
		`filter1(3.14) -> filter2("key", 42)`,
		[]*Filter{{Name: "filter1", Args: []interface{}{3.14}}, {Name: "filter2", Args: []interface{}{"key", float64(42)}}},
		false,
	}} {
		fs, err := ParseFilters(ti.expression)
		if err == nil && ti.err || err != nil && !ti.err {
			t.Error(ti.msg, "failure case", err, ti.err)
			return
		}

		checkFilters(t, ti.msg, fs, ti.check)
	}
}

func TestRouteJSON(t *testing.T) {
	for _, item := range []struct {
		route  *Route
		string string
	}{{
		&Route{},
		`{"id":"","backend":"","predicates":[],"filters":[]}` + "\n",
	}, {
		&Route{
			Filters:    []*Filter{{"xsrf", nil}},
			Predicates: []*Predicate{{"Test", nil}},
		},
		`{"id":"","backend":"","predicates":[{"name":"Test","args":[]}],"filters":[{"name":"xsrf","args":[]}]}` + "\n",
	}, {
		&Route{Method: "GET", Backend: "https://www.example.org"},
		`{"id":"","backend":"https://www.example.org","predicates":[{"name":"Method","args":["GET"]}],"filters":[]}` + "\n",
	}, {
		&Route{Method: "GET", Shunt: true},
		`{"id":"","backend":"<shunt>","predicates":[{"name":"Method","args":["GET"]}],"filters":[]}` + "\n",
	}, {
		&Route{Method: "GET", Shunt: true, BackendType: ShuntBackend},
		`{"id":"","backend":"<shunt>","predicates":[{"name":"Method","args":["GET"]}],"filters":[]}` + "\n",
	}, {
		&Route{Method: "GET", BackendType: ShuntBackend},
		`{"id":"","backend":"<shunt>","predicates":[{"name":"Method","args":["GET"]}],"filters":[]}` + "\n",
	}, {
		&Route{Method: "GET", BackendType: LoopBackend},
		`{"id":"","backend":"<loopback>","predicates":[{"name":"Method","args":["GET"]}],"filters":[]}` + "\n",
	}, {
		&Route{Method: "GET", BackendType: DynamicBackend},
		`{"id":"","backend":"<dynamic>","predicates":[{"name":"Method","args":["GET"]}],"filters":[]}` + "\n",
	}, {
		&Route{
			Method:      "PUT",
			Path:        `/some/"/path`,
			HostRegexps: []string{"h-expression", "slash/h-expression"},
			PathRegexps: []string{"p-expression", "slash/p-expression"},
			Headers: map[string]string{
				`ap"key`: `ap"value`},
			HeaderRegexps: map[string][]string{
				`ap"key`: {"slash/value0", "slash/value1"}},
			Predicates: []*Predicate{{"Test", []interface{}{3.14, "hello"}}},
			Filters: []*Filter{
				{"filter0", []interface{}{float64(3.1415), "argvalue"}},
				{"filter1", []interface{}{float64(-42), `ap"argvalue`}}},
			Shunt:   false,
			Backend: "https://www.example.org"},
		`{` +
			`"id":"",` +
			`"backend":"https://www.example.org",` +
			`"predicates":[` +
			`{"name":"Method","args":["PUT"]}` +
			`,{"name":"Path","args":["/some/\"/path"]}` +
			`,{"name":"HostRegexp","args":["h-expression"]}` +
			`,{"name":"HostRegexp","args":["slash/h-expression"]}` +
			`,{"name":"PathRegexp","args":["p-expression"]}` +
			`,{"name":"PathRegexp","args":["slash/p-expression"]}` +
			`,{"name":"Header","args":["ap\"key","ap\"value"]}` +
			`,{"name":"HeaderRegexp","args":["ap\"key","slash/value0"]}` +
			`,{"name":"HeaderRegexp","args":["ap\"key","slash/value1"]}` +
			`,{"name":"Test","args":[3.14,"hello"]}` +
			`],` +
			`"filters":[` +
			`{"name":"filter0","args":[3.1415,"argvalue"]}` +
			`,{"name":"filter1","args":[-42,"ap\"argvalue"]}` +
			`]` +
			`}` + "\n",
	}} {
		bytes, err := item.route.MarshalJSON()
		if err != nil {
			t.Error(err)
		}
		rstring := string(bytes[:])
		if rstring != item.string {
			t.Errorf("Wrong output:\n  %s\nexpected:\n  %s", rstring, item.string)
		}
	}
}

func TestPredicateParsing(t *testing.T) {
	for _, test := range []struct {
		title    string
		input    string
		expected []*Predicate
		fail     bool
	}{{
		title: "empty",
	}, {
		title: "invalid",
		input: "not predicates",
		fail:  true,
	}, {
		title:    "single predicate",
		input:    `Foo("bar")`,
		expected: []*Predicate{{Name: "Foo", Args: []interface{}{"bar"}}},
	}, {
		title: "multiple predicates",
		input: `Foo("bar") && Baz("qux") && Quux("quuz")`,
		expected: []*Predicate{
			{Name: "Foo", Args: []interface{}{"bar"}},
			{Name: "Baz", Args: []interface{}{"qux"}},
			{Name: "Quux", Args: []interface{}{"quuz"}},
		},
	}, {
		title: "star notation",
		input: `*`,
	}} {
		t.Run(test.title, func(t *testing.T) {
			p, err := ParsePredicates(test.input)

			if err == nil && test.fail {
				t.Error("failed to fail")
				return
			} else if err != nil && !test.fail {
				t.Error(err)
				return
			}

			if !reflect.DeepEqual(p, test.expected) {
				t.Error("invalid parse result")
				t.Log("got:", litter.Sdump(p))
				t.Log("expected:", litter.Sdump(test.expected))
			}
		})
	}
}

func TestClone(t *testing.T) {
	r := &Route{
		Id:            "foo",
		Path:          "/bar",
		HostRegexps:   []string{"[.]example[.]org$", "^www[.]"},
		PathRegexps:   []string{"^/", "bar$"},
		Method:        "GET",
		Headers:       map[string]string{"X-Foo": "bar"},
		HeaderRegexps: map[string][]string{"X-Bar": {"baz", "qux"}},
		Predicates:    []*Predicate{{Name: "Foo", Args: []interface{}{"bar", "baz"}}},
		Filters:       []*Filter{{Name: "foo", Args: []interface{}{42, 84}}},
		Backend:       "https://www2.example.org",
	}

	c := r.Copy()
	if c == r {
		t.Error("routes are of the same instance")
	}

	if !reflect.DeepEqual(c, r) {
		t.Error("failed to clone all the fields")
	}
}

func TestDefaultFiltersDo(t *testing.T) {
	input, err := Parse(`r1: Host("example.org") -> inlineContent("OK") -> "http://127.0.0.1:9001"`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	filter, err := ParseFilters("status(418)")
	if err != nil {
		t.Errorf("Failed to parse filter: %v", err)
	}
	filter2, err := ParseFilters("status(419)")
	if err != nil {
		t.Errorf("Failed to parse filter: %v", err)
	}

	outputPrepend, err := Parse(`r1: Host("example.org") -> status(418) -> inlineContent("OK") -> "http://127.0.0.1:9001"`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	outputAppend, err := Parse(`r1: Host("example.org")  -> inlineContent("OK") -> status(418) -> "http://127.0.0.1:9001"`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	outputPrependAppend, err := Parse(`r1: Host("example.org") -> status(419) -> inlineContent("OK") -> status(418) -> "http://127.0.0.1:9001"`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	outputPrependAppend2, err := Parse(`r1: Host("example.org") -> status(419) -> status(418) -> inlineContent("OK") -> status(418) -> status(419) -> "http://127.0.0.1:9001"`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	for _, tt := range []struct {
		name   string
		df     *DefaultFilters
		routes []*Route
		want   []*Route
	}{
		{
			name:   "test no default filters should not change anything",
			df:     &DefaultFilters{},
			routes: input,
			want:   input,
		}, {
			name: "test default filters, that are nil should not change anything",
			df: &DefaultFilters{
				Append:  nil,
				Prepend: nil,
			},
			routes: input,
			want:   input,
		}, {
			name: "test default filters, that prepend should prepend a filter",
			df: &DefaultFilters{
				Append:  nil,
				Prepend: filter,
			},
			routes: input,
			want:   outputPrepend,
		}, {
			name: "test default filters, that append should append a filter",
			df: &DefaultFilters{
				Append:  filter,
				Prepend: nil,
			},
			routes: input,
			want:   outputAppend,
		}, {
			name: "test default filters, that append and prepend should append and prepend a filter",
			df: &DefaultFilters{
				Append:  filter,
				Prepend: filter2,
			},
			routes: input,
			want:   outputPrependAppend,
		}, {
			name: "test default filters, that append and prepend should append and prepend a filters",
			df: &DefaultFilters{
				Append:  append(filter, filter2...),
				Prepend: append(filter2, filter...),
			},
			routes: input,
			want:   outputPrependAppend2,
		}} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.df.Do(tt.routes); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Want %v, got %v", tt.want, got)
			}
		})
	}

}

func TestDefaultFiltersDoCorrectPrependFilters(t *testing.T) {
	filters, err := ParseFilters("status(1) -> status(2) -> status(3)")
	if err != nil {
		t.Errorf("Failed to parse filter: %v", err)
	}

	routes, err := Parse(`
r1: Method("GET") -> inlineContent("r1") -> <shunt>;
r2: Method("POST") -> inlineContent("r2") -> <shunt>;
`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	df := &DefaultFilters{Prepend: filters}

	finalRoutes := df.Do(routes)
	for _, route := range finalRoutes {
		if route.Id != route.Filters[len(route.Filters)-1].Args[0].(string) {
			t.Errorf("Route %v has incorrect filters: %v", route.Id, route.Filters[3])
		}
	}
}

func TestEditorPreProcessor(t *testing.T) {
	r0, err := Parse(`r0: Host("www[.]example[.]org") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	r1, err := Parse(`r1_filter: Source("1.2.3.4/26") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	r1Changed, err := Parse(`r1_filter: ClientIP("1.2.3.4/26") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	rn, err := Parse(`rn_filter: Source("1.2.3.4/26", "10.5.5.0/24") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	rnChanged, err := Parse(`rn_filter: ClientIP("1.2.3.4/26", "10.5.5.0/24") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	r1Filter, err := Parse(`r1_filter: Source("1.2.3.4/26") -> uniformRequestLatency("100ms", "10ms") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	r1FilterChanged, err := Parse(`r1_filter: Source("1.2.3.4/26") -> normalRequestLatency("100ms", "10ms") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	for _, tt := range []struct {
		name   string
		rep    *Editor
		routes []*Route
		want   []*Route
	}{
		{
			name:   "test empty Editor should not change the routes",
			rep:    &Editor{},
			routes: r0,
			want:   r0,
		},
		{
			name: "test no match should not change the routes",
			rep: &Editor{
				reg:  regexp.MustCompile("SourceFromLast[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: r0,
			want:   r0,
		},
		{
			name: "test match should change the routes",
			rep: &Editor{
				reg:  regexp.MustCompile("Source[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: r1,
			want:   r1Changed,
		},
		{
			name: "test multiple routes match should change the routes",
			rep: &Editor{
				reg:  regexp.MustCompile("Source[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: append(r0, r1...),
			want:   append(r0, r1Changed...),
		},
		{
			name: "test match should change the routes with multiple params",
			rep: &Editor{
				reg:  regexp.MustCompile("Source[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: rn,
			want:   rnChanged,
		},
		{
			name: "test multiple routes match should change the routes with multiple params",
			rep: &Editor{
				reg:  regexp.MustCompile("Source[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: append(r0, rn...),
			want:   append(r0, rnChanged...),
		},
		{
			name: "test match should change the filter of a route",
			rep: &Editor{
				reg:  regexp.MustCompile("uniformRequestLatency[(](.*)[)]"),
				repl: "normalRequestLatency($1)",
			},
			routes: r1Filter,
			want:   r1FilterChanged,
		}} {
		t.Run(tt.name, func(t *testing.T) {
			r := CanonicalList(tt.routes)
			want := CanonicalList(tt.want)
			if got := tt.rep.Do(r); !reflect.DeepEqual(got, want) {
				t.Errorf("Failed to get routes %d == %d: \nwant: %v, \ngot: %v\n%s", len(want), len(got), want, got, cmp.Diff(want, got))
			}
		})
	}

}
func TestClonePreProcessor(t *testing.T) {
	r0, err := Parse(`r0: Host("www[.]example[.]org") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	r1, err := Parse(`r1: Source("1.2.3.4/26") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	r1Changed, err := Parse(`clone_r1: ClientIP("1.2.3.4/26") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	rn, err := Parse(`rn: Source("1.2.3.4/26", "10.5.5.0/24") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	rnChanged, err := Parse(`clone_rn: ClientIP("1.2.3.4/26", "10.5.5.0/24") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	r1Filter, err := Parse(`r1_filter: Source("1.2.3.4/26") -> uniformRequestLatency("100ms", "10ms") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}
	r1FilterChanged, err := Parse(`clone_r1_filter: Source("1.2.3.4/26") -> normalRequestLatency("100ms", "10ms") -> status(201) -> <shunt>`)
	if err != nil {
		t.Errorf("Failed to parse route: %v", err)
	}

	for _, tt := range []struct {
		name   string
		rep    *Clone
		routes []*Route
		want   []*Route
	}{
		{
			name:   "test empty Clone should not change the routes",
			rep:    &Clone{},
			routes: r0,
			want:   r0,
		},
		{
			name: "test no match should not change the routes",
			rep: &Clone{
				reg:  regexp.MustCompile("SourceFromLast[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: r0,
			want:   r0,
		},
		{
			name: "test match should change the routes",
			rep: &Clone{
				reg:  regexp.MustCompile("Source[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: r1,
			want:   append(r1, r1Changed...),
		},
		{
			name: "test multiple routes match should change the routes",
			rep: &Clone{
				reg:  regexp.MustCompile("Source[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: append(r0, r1...),
			want:   append(r0, append(r1, r1Changed...)...),
		},
		{
			name: "test match should change the routes with multiple params",
			rep: &Clone{
				reg:  regexp.MustCompile("Source[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: rn,
			want:   append(rn, rnChanged...),
		},
		{
			name: "test multiple routes match should change the routes with multiple params",
			rep: &Clone{
				reg:  regexp.MustCompile("Source[(](.*)[)]"),
				repl: "ClientIP($1)",
			},
			routes: append(r0, rn...),
			want:   append(r0, append(rn, rnChanged...)...),
		},
		{
			name: "test match should change the filter of a route",
			rep: &Clone{
				reg:  regexp.MustCompile("uniformRequestLatency[(](.*)[)]"),
				repl: "normalRequestLatency($1)",
			},
			routes: r1Filter,
			want:   append(r1Filter, r1FilterChanged...),
		}} {
		t.Run(tt.name, func(t *testing.T) {
			r := CanonicalList(tt.routes)
			want := CanonicalList(tt.want)
			if got := tt.rep.Do(r); !reflect.DeepEqual(got, want) {
				t.Errorf("Failed to get routes %d == %d: \nwant: %v, \ngot: %v\n%s", len(want), len(got), want, got, cmp.Diff(want, got))
			}
		})
	}

}

func TestPredicateString(t *testing.T) {
	for _, tt := range []struct {
		name      string
		predicate *Predicate
		want      string
	}{
		{
			name: "test one parameter",
			predicate: &Predicate{
				Name: "ClientIP",
				Args: []interface{}{
					"1.2.3.4/26",
				},
			},
			want: `ClientIP("1.2.3.4/26")`,
		},
		{
			name: "test two parameters",
			predicate: &Predicate{
				Name: "ClientIP",
				Args: []interface{}{
					"1.2.3.4/26",
					"10.2.3.4/22",
				},
			},
			want: `ClientIP("1.2.3.4/26", "10.2.3.4/22")`,
		}} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.predicate.String()
			if got != tt.want {
				t.Errorf("Failed to String(): Want %v, got %v", tt.want, got)
			}
		})
	}
}

func TestFilterString(t *testing.T) {
	for _, tt := range []struct {
		name   string
		filter *Filter
		want   string
	}{
		{
			name: "test one parameter",
			filter: &Filter{
				Name: "setPath",
				Args: []interface{}{
					"/foo",
				},
			},
			want: `setPath("/foo")`,
		},
		{
			name: "test two parameters",
			filter: &Filter{
				Name: "uniformRequestLatency",
				Args: []interface{}{
					"100ms",
					"10ms",
				},
			},
			want: `uniformRequestLatency("100ms", "10ms")`,
		}} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.String()
			if got != tt.want {
				t.Errorf("Failed to String(): Want %v, got %v", tt.want, got)
			}
		})
	}
}
