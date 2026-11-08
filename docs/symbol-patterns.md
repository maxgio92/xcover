# Symbol patterns

How `--include` and `--exclude` patterns meet C++ and Rust names. Both flags
take a Go (RE2) regular expression, matched against the raw symbol name and
the demangled name, so `^app::net::` and `^_ZN3app` select the same
namespace. Exclude wins over include. Names come from
[github.com/ianlancetaylor/demangle](https://github.com/ianlancetaylor/demangle).

RE2 has no negative lookahead, so express "everything but" with `--exclude`
or `--scope` rather than a lookahead in `--include`.

## C++

The output is close to `c++filt` but keeps typedefs unexpanded: the library
prints `operator<<(std::ostream&, Foo const&)` where `c++filt` expands to
`std::basic_ostream<char, std::char_traits<char> >&`.

Overloads keep their parameter list (`app::net::parse(int)`). Template
instantiations start with the return type
(`double app::net::twice<double>(double)`), so leave the pattern unanchored
to catch them.

An unanchored pattern runs over the whole signature, parameter types
included, so `app::net::` also matches a function elsewhere that takes an
`app::net` type. Anchor on the raw name (`^_ZN[KVRO]*3app3net`) when that
matters.

## Rust

The demangler drops the legacy hash suffix (`::h5d6b4c8a0f1e2d3b`) and the v0
crate disambiguator (`[3c1c0]`), so several monomorphizations share one name
and a pattern must leave both out.

v0 names wrap the type a method belongs to in angle brackets: an inherent
method renders as `<mycrate::net::Conn>::open` and a closure inside it as
`<mycrate::net::Conn>::open::{closure#0}`. A legacy inherent method renders
as `mycrate::net::Conn::open`.

Free functions, their closures (`mycrate::net::parse::{closure#0}`) and
generic instantiations (`mycrate::net::twice::<i32>`) keep the `mycrate::`
prefix. `^mycrate::` therefore misses every v0 method and the closures inside
them; `^<?mycrate::` catches those too.

A trait impl for a type from another crate renders as
`<i32 as mycrate::net::MyTrait>::run`, which neither anchored pattern
matches. Leave the pattern unanchored (`mycrate::`) to catch it, with the
same false-positive caveat as for C++.
