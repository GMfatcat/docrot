//! HTTP routes (axum).
use axum::{routing::get, Router};

/// router builds the fixture's routes.
pub fn router() -> Router {
    Router::new()
        .route("/rs/items", get(list).post(create))
        .route("/rs/items/:id", get(one))
}

async fn list() {}
async fn create() {}
async fn one() {}
