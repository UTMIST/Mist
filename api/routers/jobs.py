from fastapi import APIRouter

router = APIRouter()

@router.post("/jobs")
def enqueue_job():
    pass

@router.get("/jobs/{job_id}/status")
def get_job_status(job_id):
    pass

    