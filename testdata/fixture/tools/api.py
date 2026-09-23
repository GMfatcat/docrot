"""Fixture FastAPI-style app."""

from fastapi import APIRouter, FastAPI

app = FastAPI()
router = APIRouter(prefix="/py")


@app.get("/py/items")
async def list_items():
    return []


@router.post(
    "/items/{item_id}",
    response_model=dict,
)
async def update_item(item_id: int):
    return {}
