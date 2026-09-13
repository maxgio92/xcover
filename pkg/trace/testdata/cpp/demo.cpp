// Fixture for the demangling tests (maxgio92/xcover#192). Built by the
// integration tests with: g++ -O0 -g -Wl,--build-id -o demo demo.cpp
#include <cstdio>
namespace app { namespace net {
  int parse(const char*) { return 1; }
  int parse(int) { return 2; }
  struct Conn { void open(); void close(); };
  void Conn::open() {}
  void Conn::close() {}
  template <typename T> T twice(T v) { return v + v; }
}}
static int helper_static(int x) { return x * 3; }
extern "C" int c_entry(int x) { return x; }
int main() {
  app::net::Conn c; c.open(); c.close();
  std::printf("%d %d %d %d %d\n", app::net::parse("x"), app::net::parse(1),
    app::net::twice(2), app::net::twice(2.5) > 0, helper_static(1) + c_entry(1));
  return 0;
}
