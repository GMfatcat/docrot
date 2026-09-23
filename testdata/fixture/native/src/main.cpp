#include <CLI/CLI.hpp>
#include "fix/fix.h"

namespace fix {

/// Engine drives the fixture.
class Engine {
 public:
  /// start begins work.
  void start();
  int workers() const { return 2; }
};

}  // namespace fix

int main(int argc, char **argv)
{
  CLI::App app{"fixture"};
  int threads = 2;
  app.add_option("--threads", threads)->default_val(2);
  app.add_flag("--quiet", "say less");
  return 0;
}
