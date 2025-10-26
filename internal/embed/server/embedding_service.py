import sys
import types

# WORKAROUND: PyTorch embedded environment compatibility fix
#
# When using pip install --target for embedded Python (as done by go-embed-python),
# PyTorch's test modules like torch._inductor.test_operators are not included.
# However, torch._dynamo.trace_rules unconditionally tries to import test_operators
# during module initialization, causing a ModuleNotFoundError.
#
# Since we only need PyTorch for inference (not training or compilation), we can
# safely create a dummy test_operators module. This prevents the import error
# without affecting any inference functionality.
#
# This must be done BEFORE any torch imports occur.
dummy_test_operators = types.ModuleType('torch._inductor.test_operators')
sys.modules['torch._inductor.test_operators'] = dummy_test_operators

from fastapi import FastAPI, Request
from pydantic import BaseModel
from sentence_transformers import SentenceTransformer
from typing import List, Literal, Dict
from pathlib import Path
import uvicorn

# Model configuration
MODEL_NAME = "BAAI/bge-small-en-v1.5"
MODEL_INFO = {
    "name": MODEL_NAME,
    "dimensions": 384,
    "max_tokens": 512
}

# Check if model is already cached
cache_dir = Path.home() / ".cache" / "huggingface" / "hub" / f"models--{MODEL_NAME.replace('/', '--')}"
is_cached = cache_dir.exists()

# Load model on startup
if is_cached:
    print(f"Loading {MODEL_NAME}...", flush=True)
else:
    print(f"Loading {MODEL_NAME}...", flush=True)
    print("(First run: downloading ~130MB from HuggingFace)", flush=True)

model = SentenceTransformer(MODEL_NAME, device='cpu')
print("✓ Model ready", flush=True)

app = FastAPI()

class EmbedRequest(BaseModel):
    texts: List[str]
    mode: Literal["query", "passage"] = "passage"  # default to 'passage' for document chunking

@app.post("/embed")
def embed(req: EmbedRequest):
    # BGE models don't require prefixes
    # They handle query vs passage internally
    vectors = model.encode(req.texts, normalize_embeddings=True).tolist()
    return {"embeddings": vectors}

@app.get("/")
def health() -> Dict:
    return {
        "status": "ok",
        "model": MODEL_INFO["name"],
        "dimensions": MODEL_INFO["dimensions"],
        "max_tokens": MODEL_INFO["max_tokens"]
    }

@app.get("/model_info")
def model_info() -> Dict:
    return MODEL_INFO

if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=8121)