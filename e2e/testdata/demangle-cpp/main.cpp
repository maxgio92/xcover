// Fixture for the demangle e2e scenario. ns::compute lives in a namespace so
// its symbol is mangled and the report must carry a demangled name for it.
#include <cstdio>

namespace ns {
__attribute__((noinline)) int compute(int x) { return x * 2 + 1; }
}  // namespace ns

int main() {
  volatile int result = ns::compute(20);
  std::printf("%d\n", result);
  return 0;
}
