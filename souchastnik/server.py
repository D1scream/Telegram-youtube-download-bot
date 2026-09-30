import json
import math
import os
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


ROOT = Path(__file__).parent
LLAMA_URL = os.environ.get("LLAMA_URL", "http://llama:8080/completion")
MIN_CONFIDENCE = float(os.environ.get("MIN_CONFIDENCE", "0"))


with (ROOT / "assets" / "articles.json").open(encoding="utf-8") as file:
    articles = {
        item["code"]: item
        for item in json.load(file)["articles"]
        if item.get("kind") != "aggravating"
    }
judge_template = (ROOT / "assets" / "judge.txt").read_text(encoding="utf-8")
examples_path = ROOT / "assets" / "examples.json"
examples = json.loads(examples_path.read_text(encoding="utf-8")) if examples_path.exists() else {}
shots: dict[str, list[str]] = examples.get("shots", {})
clean: list[str] = examples.get("clean", []) + [
    line.split("»")[0].lstrip("«")
    for line in examples.get("gate", [])
    if line.endswith("-> нет")
]
codes = sorted(articles, key=len, reverse=True)


def chat_turn(user: str, assistant: str) -> str:
    return (
        f"<|im_start|>user\n{user}<|im_end|>\n"
        f"<|im_start|>assistant\n<think>\n\n</think>\n\n{assistant}<|im_end|>\n"
    )


def build_system() -> str:
    table = "\n".join(
        f'{code} {article["act"]} — {article["title"]}' for code, article in articles.items()
    )
    system = judge_template.replace("@@TABLE@@", table)
    system += (
        "\n\nНарушение может быть завуалировано: эвфемизмы, намеки, искаженное написание. "
        "Оценивай смысл, а не отдельные слова."
        "\n\nПеред сообщением может идти предыдущая переписка из чата. "
        "Она нужна только чтобы понять смысл, оценивай только само сообщение."
    )
    positive = [(shot, code) for code, items in shots.items() for shot in items[:1]]
    negative = [(text, "none") for text in clean]
    turns = []
    while positive or negative:
        if positive:
            turns.append(positive.pop(0))
        if negative:
            turns.append(negative.pop(0))
    return f"<|im_start|>system\n{system}<|im_end|>\n" + "".join(
        chat_turn(user, assistant) for user, assistant in turns
    )


system_prompt = build_system()


def build_prompt(text: str, context: list[str]) -> str:
    message = text
    if context:
        message = "Предыдущая переписка:\n" + "\n".join(context) + f"\n\nСообщение:\n{text}"
    return (
        system_prompt
        + f"<|im_start|>user\n{message}<|im_end|>\n"
        "<|im_start|>assistant\n<think>\n\n</think>\n\n"
    )


def verdict(text: str, context: list[str]) -> str:
    payload = json.dumps(
        {
            "prompt": build_prompt(text, context),
            "n_predict": 8,
            "temperature": 0,
            "n_probs": 10,
            "cache_prompt": True,
        }
    ).encode()
    request = urllib.request.Request(
        LLAMA_URL,
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=120) as response:
        result = json.loads(response.read())
    generated = result.get("content", "").strip()
    code = next((code for code in codes if generated.startswith(code)), "none")
    if code != "none" and confidence(result) < MIN_CONFIDENCE:
        return "none"
    return code


def confidence(result: dict) -> float:
    probabilities = result.get("completion_probabilities") or [{}]
    top = probabilities[0].get("top_logprobs") or probabilities[0].get("probs") or []
    none_probability = sum(
        math.exp(item["logprob"]) if "logprob" in item else item.get("prob", 0)
        for item in top
        if (item.get("token") or item.get("tok_str") or "").strip().lower().startswith("n")
    )
    return 1 - none_probability


def description(code: str) -> str:
    article = articles.get(code)
    if article is None:
        return ""
    return f'Статья {code} {article["act"]}. {article["title"].capitalize()}. Наказание: {article["penalty"]}'


class Handler(BaseHTTPRequestHandler):
    def do_POST(self) -> None:
        if self.path != "/verdict":
            self.send_error(404)
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            request = json.loads(self.rfile.read(length))
            text = request.get("text", "")
            context = [str(line) for line in request.get("context") or []]
            code = verdict(str(text), context)
            result = {"code": code}
            if code != "none":
                result["description"] = description(code)
            body = json.dumps(result).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        except Exception as error:
            self.send_error(500, str(error))

    def log_message(self, *_args: object) -> None:
        return


ThreadingHTTPServer(("0.0.0.0", 8081), Handler).serve_forever()
