package py

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDefaults(t *testing.T) {
	src := `
def main(
    port: int = typer.Option(8080, "--port"),
    name: str = typer.Argument("World"),
    factory: str = typer.Option(default_factory=get),
    flag: bool = typer.Option(False),
    dry_run: bool = typer.Option(...),
):
    pass

@click.option("--workers", default=4, help="n")
@click.option("--mode", default="fast", type=str)
parser.add_argument("--level", default="info", help="x")
parser.add_argument("--n", type=int)
host = os.getenv("APP_HOST", "0.0.0.0")
port = os.environ.get("APP_PORT", 8000)
x = os.getenv("NO_DEFAULT")
`
	got := parseDefaults(strings.Split(src, "\n"))
	want := [][3]string{
		{"flag", "port", "8080"}, {"flag", "name", "World"}, {"flag", "flag", "false"},
		{"flag", "--workers", "4"}, {"flag", "--mode", "fast"}, {"flag", "--level", "info"},
		{"env", "APP_HOST", "0.0.0.0"}, {"env", "APP_PORT", "8000"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("defaults =\n  %q\nwant\n  %q", got, want)
	}
}
