from fastapi import APIRouter

router = APIRouter()

@router.post("/auth/login")
def login():
    pass

@router.post("/auth/refresh")
def refresh():
    pass

    