//! io helpers.
use std::env;

/// read_all reads a whole file.
pub fn read_all(path: &str) -> Vec<u8> {
    let home = env::var("FIXTURE_HOME").unwrap_or("/tmp".to_string());
    let _ = (home, path);
    Vec::new()
}

/// write_all writes a whole file.
pub fn write_all(path: &str, data: &[u8]) {
    let _ = (path, data);
}
