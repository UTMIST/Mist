from fastapi import FastAPI
from .routers import auth, jobs

app = FastAPI()

app.include_router(auth.router)
app.include_router(jobs.router)

@app.get("/")
def root():
    return "Hello, world!"
