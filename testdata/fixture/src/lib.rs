//! Fixture crate: a small Rust surface for docrot's golden tests.

pub mod io;
pub mod server;

use clap::Parser;

/// Args are the command-line options.
#[derive(Parser, Debug)]
pub struct Args {
    /// Log level.
    #[arg(long, env = "FIXTURE_LEVEL", default_value = "info")]
    pub level: String,
    /// Worker count.
    #[arg(long, default_value_t = 4)]
    pub workers: usize,
}

/// Config holds the runtime settings.
#[derive(Debug, Clone)]
pub struct Config {
    pub level: String,
}

impl Config {
    /// new builds a Config from Args.
    pub fn new(args: &Args) -> Self {
        Config { level: args.level.clone() }
    }
}

/// render draws one frame.
pub fn render(frame: &mut [u8]) -> usize {
    frame.len()
}
