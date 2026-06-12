from huggingface_hub import snapshot_download

snapshot_download(
    "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2",
    cache_dir="/data/hub",
)
