package runner

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/AtomyCloud/atm/internal/plugin"
)

func TestNativeArgumentSemanticsBelongToBackend(t *testing.T) {
	c, _ := nativePlan(t)
	plugins, err := plugin.Discover(c, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	p := plugins[0]
	p.Operations = []string{"menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"}
	for _, op := range p.Operations {
		for _, args := range [][]string{nil, {"1.2.3", "--location", "local", "--trust-dependencies"}, {"literal; $(not-a-command)"}} {
			t.Run(op, func(t *testing.T) {
				plan, err := Plugin(c, p, op, args)
				if err != nil || !reflect.DeepEqual(plan.Arguments, args) {
					t.Fatalf("native arguments changed/refused: %+v %v", plan, err)
				}
				if plan.Executor != "native" || len(plan.Command) != 1 {
					t.Fatal("native arguments entered shell command")
				}
			})
		}
	}
	if _, err := Plugin(c, p, "use", make([]string, 129)); err == nil {
		t.Fatal("native argument limit bypassed")
	}
	if _, err := Plugin(c, p, "undeclared", nil); err == nil {
		t.Fatal("undeclared operation accepted")
	}
	p.Executor = "legacy-bash"
	for _, op := range []string{"use", "remove", "menu", "status", "uninstall"} {
		if _, err := Plugin(c, p, op, []string{"1.2.3", "--location", "local"}); err == nil {
			t.Fatalf("legacy %s arity guard lost", op)
		}
	}
}
