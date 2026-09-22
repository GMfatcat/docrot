package extract

// stdlibPackages lists the package *names* (last path element) of the Go
// standard library. A dotted reference whose first part is one of these is
// never treated as a symbol of the module under inspection.
var stdlibPackages = map[string]bool{}

func init() {
	for _, n := range []string{
		"archive", "tar", "zip", "bufio", "builtin", "bytes", "cmp", "compress",
		"bzip2", "flate", "gzip", "lzw", "zlib", "container", "heap", "list",
		"ring", "context", "crypto", "aes", "cipher", "des", "dsa", "ecdh",
		"ecdsa", "ed25519", "elliptic", "hmac", "md5", "rand", "rc4", "rsa",
		"sha1", "sha256", "sha512", "sha3", "subtle", "tls", "x509", "pkix",
		"database", "sql", "driver", "debug", "dwarf", "elf", "gosym", "macho",
		"pe", "plan9obj", "embed", "encoding", "ascii85", "asn1", "base32",
		"base64", "binary", "csv", "gob", "hex", "json", "pem", "xml", "errors",
		"expvar", "flag", "fmt", "go", "ast", "build", "constant", "doc",
		"format", "importer", "parser", "printer", "scanner", "token", "types",
		"version", "hash", "adler32", "crc32", "crc64", "fnv", "maphash", "html",
		"template", "image", "color", "palette", "draw", "gif", "jpeg", "png",
		"index", "suffixarray", "io", "fs", "iter", "log", "slog", "syslog",
		"maps", "math", "big", "bits", "cmplx", "mime", "multipart",
		"quotedprintable", "net", "http", "cgi", "cookiejar", "fcgi", "httptest",
		"httptrace", "httputil", "pprof", "mail", "netip", "rpc", "jsonrpc",
		"smtp", "textproto", "url", "os", "exec", "signal", "user", "path",
		"filepath", "plugin", "reflect", "regexp", "syntax", "runtime", "cgo",
		"coverage", "debug", "metrics", "race", "trace", "slices", "sort",
		"strconv", "strings", "structs", "sync", "atomic", "syscall", "js",
		"testing", "fstest", "iotest", "quick", "slogtest", "synctest", "text",
		"tabwriter", "time", "tzdata", "unicode", "utf16", "utf8", "unique",
		"unsafe", "weak",
	} {
		stdlibPackages[n] = true
	}
}

// IsStdlibPackage reports whether name is the name of a standard library
// package (e.g. "http", "slog", "context").
func IsStdlibPackage(name string) bool { return stdlibPackages[name] }
