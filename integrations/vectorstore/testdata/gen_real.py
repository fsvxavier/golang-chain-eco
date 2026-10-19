# Regenerates the real_* fixtures with the official libraries:
#   pip install annoy faiss-cpu numpy && python gen_real.py .
import sys, json, numpy as np, annoy, faiss
out = sys.argv[1]
rng = np.random.default_rng(7)
q = {}
def top(scores, ids, k=5, desc=True):
    o = sorted(range(len(scores)), key=lambda i: (-scores[i] if desc else scores[i], ids[i]))
    return [int(ids[i]) for i in o[:k]]
for metric, dim in (("angular", 8), ("euclidean", 5)):
    X = rng.standard_normal((40, dim)).astype(np.float32)
    t = annoy.AnnoyIndex(dim, metric)
    for i, v in enumerate(X): t.add_item(i, v.tolist())
    t.build(4); t.save(f"{out}/real_{metric}.ann")
    qv = rng.standard_normal(dim).astype(np.float32)
    # exact neighbours by annoy itself with huge search_k
    q[f"annoy_{metric}"] = {"dim": dim, "query": qv.tolist(), "top": t.get_nns_by_vector(qv.tolist(), 5, search_k=100000),
                            "item3": t.get_item_vector(3)}
d = 6
X = rng.standard_normal((30, d)).astype(np.float32)
qv = rng.standard_normal((1, d)).astype(np.float32)
for name, idx in (("l2", faiss.IndexFlatL2(d)), ("ip", faiss.IndexFlatIP(d))):
    idx.add(X); faiss.write_index(idx, f"{out}/real_flat_{name}.faiss")
    D_, I_ = idx.search(qv, 5)
    q[f"faiss_{name}"] = {"query": qv[0].tolist(), "top": I_[0].tolist(), "scores": D_[0].tolist()}
m = faiss.IndexIDMap(faiss.IndexFlatL2(d))
ids = np.arange(1000, 1000 + 30 * 7, 7).astype(np.int64)
m.add_with_ids(X, ids); faiss.write_index(m, f"{out}/real_idmap_l2.faiss")
D_, I_ = m.search(qv, 5)
q["faiss_idmap"] = {"query": qv[0].tolist(), "top": I_[0].tolist(), "scores": D_[0].tolist()}
json.dump(q, open(f"{out}/real_expected.json", "w"), indent=1)
