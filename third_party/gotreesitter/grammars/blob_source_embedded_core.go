//go:build !grammar_blobs_external && grammar_set_core

package grammars

import "embed"

//go:embed grammar_blobs/bash.bin
//go:embed grammar_blobs/c.bin
//go:embed grammar_blobs/cpp.bin
//go:embed grammar_blobs/c_sharp.bin
//go:embed grammar_blobs/cmake.bin
//go:embed grammar_blobs/css.bin
//go:embed grammar_blobs/dart.bin
//go:embed grammar_blobs/elixir.bin
//go:embed grammar_blobs/elm.bin
//go:embed grammar_blobs/erlang.bin
//go:embed grammar_blobs/go.bin
//go:embed grammar_blobs/gomod.bin
//go:embed grammar_blobs/graphql.bin
//go:embed grammar_blobs/haskell.bin
//go:embed grammar_blobs/hcl.bin
//go:embed grammar_blobs/html.bin
//go:embed grammar_blobs/ini.bin
//go:embed grammar_blobs/java.bin
//go:embed grammar_blobs/javascript.bin
//go:embed grammar_blobs/json.bin
//go:embed grammar_blobs/json5.bin
//go:embed grammar_blobs/julia.bin
//go:embed grammar_blobs/kotlin.bin
//go:embed grammar_blobs/lua.bin
//go:embed grammar_blobs/make.bin
//go:embed grammar_blobs/markdown.bin
//go:embed grammar_blobs/nix.bin
//go:embed grammar_blobs/objc.bin
//go:embed grammar_blobs/ocaml.bin
//go:embed grammar_blobs/perl.bin
//go:embed grammar_blobs/php.bin
//go:embed grammar_blobs/powershell.bin
//go:embed grammar_blobs/python.bin
//go:embed grammar_blobs/r.bin
//go:embed grammar_blobs/ruby.bin
//go:embed grammar_blobs/rust.bin
//go:embed grammar_blobs/scala.bin
//go:embed grammar_blobs/scss.bin
//go:embed grammar_blobs/sql.bin
//go:embed grammar_blobs/svelte.bin
//go:embed grammar_blobs/swift.bin
//go:embed grammar_blobs/toml.bin
//go:embed grammar_blobs/tsx.bin
//go:embed grammar_blobs/typescript.bin
//go:embed grammar_blobs/xml.bin
//go:embed grammar_blobs/yaml.bin
//go:embed grammar_blobs/zig.bin
//go:embed grammar_blobs/awk.bin
//go:embed grammar_blobs/clojure.bin
//go:embed grammar_blobs/d.bin
//go:embed grammar_blobs/regex.bin
//go:embed grammar_blobs/agda.bin
//go:embed grammar_blobs/embedded_template.bin
//go:embed grammar_blobs/verilog.bin
//go:embed grammar_blobs/ada.bin
//go:embed grammar_blobs/angular.bin
//go:embed grammar_blobs/apex.bin
//go:embed grammar_blobs/arduino.bin
//go:embed grammar_blobs/asm.bin
//go:embed grammar_blobs/astro.bin
//go:embed grammar_blobs/authzed.bin
//go:embed grammar_blobs/bass.bin
//go:embed grammar_blobs/beancount.bin
//go:embed grammar_blobs/bibtex.bin
//go:embed grammar_blobs/bicep.bin
//go:embed grammar_blobs/bitbake.bin
//go:embed grammar_blobs/blade.bin
//go:embed grammar_blobs/brightscript.bin
//go:embed grammar_blobs/caddy.bin
//go:embed grammar_blobs/cairo.bin
//go:embed grammar_blobs/capnp.bin
//go:embed grammar_blobs/chatito.bin
//go:embed grammar_blobs/circom.bin
//go:embed grammar_blobs/comment.bin
//go:embed grammar_blobs/commonlisp.bin
//go:embed grammar_blobs/cooklang.bin
//go:embed grammar_blobs/corn.bin
//go:embed grammar_blobs/cpon.bin
//go:embed grammar_blobs/csv.bin
//go:embed grammar_blobs/cuda.bin
//go:embed grammar_blobs/cue.bin
//go:embed grammar_blobs/cylc.bin
//go:embed grammar_blobs/desktop.bin
//go:embed grammar_blobs/devicetree.bin
//go:embed grammar_blobs/diff.bin
//go:embed grammar_blobs/disassembly.bin
//go:embed grammar_blobs/djot.bin
//go:embed grammar_blobs/dockerfile.bin
//go:embed grammar_blobs/doxygen.bin
//go:embed grammar_blobs/dtd.bin
//go:embed grammar_blobs/earthfile.bin
//go:embed grammar_blobs/ebnf.bin
//go:embed grammar_blobs/editorconfig.bin
//go:embed grammar_blobs/eds.bin
//go:embed grammar_blobs/eex.bin
//go:embed grammar_blobs/elsa.bin
//go:embed grammar_blobs/enforce.bin
//go:embed grammar_blobs/facility.bin
//go:embed grammar_blobs/faust.bin
//go:embed grammar_blobs/fennel.bin
var grammarBlobFS embed.FS

func readGrammarBlob(blobName string) (grammarBlob, error) {
	data, err := grammarBlobFS.ReadFile("grammar_blobs/" + blobName)
	if err != nil {
		return grammarBlob{}, err
	}
	return grammarBlob{data: data}, nil
}
