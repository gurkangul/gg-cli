# GG — Shared Brain for AI Agents

## Vision

AI agent'lar (Claude Code, GSD2, BMAD, Codex, vb.) kendi terminallerinde bağımsız çalışır ama hepsi aynı bilgi tabanını kullanır. Orchestrator yok, daemon yok, UI yok. Sadece bir CLI + Qdrant + Memgraph.

**Tagline:** "One brain, any agent."

---

## Problem

- Her agent kendi klasöründe izole çalışır, birbirinden habersiz
- Bir agent'ın aldığı kararı diğeri bilmiyor
- Reddedilen yaklaşımlar tekrarlanıyor
- Kod yapısı hakkında bilgi her seferinde sıfırdan keşfediliyor
- Agent'lar arası iletişim yok

## Çözüm

Tek bir CLI tool (`gg`) + iki veritabanı (Qdrant + Memgraph, Docker'da lokal):

- **Qdrant** → kararlar, tasklar, mesajlar, reddedilenler (semantic search)
- **Memgraph** → kod yapısı, dosya ilişkileri, bağımlılık graph'i

Her agent'ın rules dosyasına aynı kurallar inject edilir. Agent otomatik olarak `gg` CLI'ı çağırarak ortak beyni kullanır. Kullanıcı sadece agent ile konuşur, `gg` komutlarını agent çalıştırır.

---

## Tech Stack

| Bileşen          | Teknoloji                           |
| ---------------- | ----------------------------------- |
| CLI              | Go                                  |
| Semantic Storage | Qdrant (Docker)                     |
| Code Graph       | Memgraph (Docker)                   |
| Embedding        | OpenAI API veya local (nomic-embed) |
| Config           | YAML                                |

---

## Proje İçi Yapı

```
.gg/
  RULES.md                ← agent kuralları (tek kaynak, her agent'a inject)
  config.yaml             ← qdrant/memgraph bağlantı ayarları
  docker-compose.yaml     ← Qdrant + Memgraph
  volumes/
    qdrant/               ← Qdrant data (projeye özel, taşınabilir)
    memgraph/             ← Memgraph data (projeye özel, taşınabilir)
```

Başka dosya yok. Task yok, karar dosyası yok, session yok. Her şey veritabanında.

---

## CLI Komutları

### Oturum

```bash
gg init              # .gg/ oluştur, docker-compose up, ilk index
gg status            # açık tasklar, bekleyen mesajlar, son kararlar
```

### Kararlar

```bash
gg decide "JWT kullanılacak" --reason "stateless, mobile uyumlu" --tags "auth,backend"
gg search "authentication"          # semantic arama
gg reject "session-based auth" --reason "stateful, ölçeklenmiyor" --task "TASK-001"
```

### Tasklar

```bash
gg task create "JWT auth endpoint" --detail "login, register, refresh" --priority high --tags "auth"
gg task list                        # tüm tasklar (filtreleme: --status pending/done/blocked)
gg task get TASK-001                # detay + ilgili kararlar + etkilenen dosyalar
gg task done TASK-001 "JWT auth implemented, test yazıldı"
gg task block TASK-001 "payment API key eksik"
```

### Code Intelligence

```bash
gg index                            # tüm codebase'i Memgraph'a indexle
gg index --changed                  # sadece son commit'teki değişiklikleri indexle
gg impact src/auth/login.ts         # bu dosya değişirse ne etkilenir
```

### Agent İletişim

```bash
gg tell "developer" "auth modülü hazır, JWT 1 saat expire"
gg tell "qa" "login endpoint'te rate limiting test edilmeli"
gg inbox                            # sana gelen mesajlar
gg inbox --role developer           # role göre filtrele
```

---

## Veri Modeli

### Qdrant Collections

```
decisions
  ├── id: uuid
  ├── text: "JWT based authentication kullanılacak"
  ├── reason: "stateless, mobile uyumlu, microservice ready"
  ├── tags: ["auth", "backend"]
  ├── task_id: "TASK-001" (nullable)
  ├── created_at: timestamp
  └── embedding: vector

tasks
  ├── id: "TASK-001"
  ├── title: "JWT auth endpoint"
  ├── detail: "login, register, refresh token implement et"
  ├── status: "pending" | "in_progress" | "done" | "blocked"
  ├── priority: "high" | "medium" | "low"
  ├── depends_on: ["TASK-000"]
  ├── tags: ["auth", "backend"]
  ├── block_reason: null
  ├── done_summary: null
  ├── created_at: timestamp
  └── embedding: vector

messages
  ├── id: uuid
  ├── from_role: "architect"
  ├── to_role: "developer"
  ├── content: "auth modülü hazır, JWT 1 saat expire"
  ├── read: false
  ├── task_id: "TASK-001" (nullable)
  └── created_at: timestamp

rejections
  ├── id: uuid
  ├── approach: "session-based authentication"
  ├── reason: "stateful, horizontal scaling zorlaştırır"
  ├── task_id: "TASK-001" (nullable)
  ├── created_at: timestamp
  └── embedding: vector
```

### Memgraph Schema

```cypher
// Node tipleri
(:File {path: "src/auth/login.ts", language: "typescript", last_indexed: timestamp})
(:Function {name: "handleLogin", file: "src/auth/login.ts", line: 42})
(:Module {name: "auth", path: "src/auth/"})
(:Package {name: "jsonwebtoken", version: "9.0.0"})

// İlişkiler
(File)-[:IMPORTS]->(File)
(File)-[:BELONGS_TO]->(Module)
(Function)-[:CALLS]->(Function)
(Function)-[:DEFINED_IN]->(File)
(File)-[:USES_PACKAGE]->(Package)
(Module)-[:DEPENDS_ON]->(Module)
```

---

## RULES.md (Agent Kuralları)

```markdown
# GG KURALLARI

Sen bu projede paylaşılan bir bilgi tabanı ile çalışıyorsun.
Tüm kararlar, tasklar ve bilgi alışverişi gg CLI üzerinden yapılır.
Kullanıcı senden gg çalıştırmanı ASLA istemez — sen otomatik çalıştırırsın.

## OTURUM BAŞLANGIÇ

Her konuşma başladığında ilk iş:
gg status
Açık tasklar, bekleyen mesajlar, son kararlar özetini al ve kullanıcıya bildir.

## TARTIŞMA SIRASINDA

Kullanıcıyla bir konu tartışırken:

1. Konu hakkında daha önce karar alınmış mı:
   gg search "konu"
2. Reddedilmiş yaklaşım var mı kontrol et
3. Varsa kullanıcıya bildir

## KARAR ANI

Kullanıcı ile bir karara vardığında (açık veya üstü kapalı):

- "JWT kullanalım" → karar
- "tamam öyle yapalım" → önceki önerinin onayı = karar
- "evet mantıklı" → karar

Tespit ettiğinde:
gg decide "kısa karar" --reason "sebep" --tags "etiketler"
Kullanıcıya: "Karar olarak kaydettim."

## TASK OLUŞTURMA

Bir iş yapılması gerektiği netleştiğinde:
gg task create "başlık" --detail "açıklama" --priority high --tags "etiketler"
Kullanıcıya: "Task açtım: TASK-XXX"

## TASK ÇÖZME

Kullanıcı "taskları çöz" veya "TASK-XXX'i yap" dediğinde:

1. gg task list --status pending
2. Her task için:
   a. gg task get TASK-XXX
   b. gg impact "etkilenecek dosyalar"
   c. Kodu yaz, test et, commit at
   d. gg task done TASK-XXX "özet"

## DOSYA DEĞİŞTİRMEDEN ÖNCE

Her zaman:
gg impact src/dosya/yolu.ts

## BAŞKA AGENT'A MESAJ

Bir iş başka role kalıyorsa:
gg tell "hedef-rol" "mesaj"

## HATA / BLOCKER

Task çözülemiyorsa:
gg task block TASK-XXX "sebep"

## REDDEDİLEN YAKLAŞIMLAR

Yaklaşım reddedildiğinde:
gg reject "yaklaşım" --reason "neden"

## ASLA YAPMA

- gg olmadan karar alma
- Reddedilmiş yaklaşımı tekrarlama
- Task açmadan "sonra yaparız" deme
- impact kontrolü yapmadan dosya değiştirme
- Kullanıcıdan gg komutu çalıştırmasını isteme
```

---

## Git Hooks

```bash
# .git/hooks/post-commit
#!/bin/sh
gg index --changed

# .git/hooks/pre-push
#!/bin/sh
gg check  # kaydedilmemiş karar veya açık task uyarısı
```

---

## Faz 1 — Core CLI (Hafta 1)

- [ ] Go project init + CLI framework (cobra)
- [ ] `gg init` — .gg/ dizin oluştur, docker-compose up, config.yaml
- [ ] Qdrant bağlantısı + collection setup
- [ ] Embedding generation (OpenAI API veya local)
- [ ] `gg decide` / `gg search` / `gg reject`
- [ ] `gg task create/list/get/done/block`
- [ ] `gg tell` / `gg inbox`
- [ ] `gg status`
- [ ] docker-compose.yaml (Qdrant + Memgraph)
- [ ] RULES.md template generation

**Deliverable:** Agent'lar karar alıp, task açıp, mesajlaşabilir.

## Faz 2 — Code Intelligence (Hafta 2)

- [ ] Memgraph bağlantısı + schema setup
- [ ] `gg index` — AST parse (TypeScript, Go, Python) → Memgraph
- [ ] `gg index --changed` — incremental index
- [ ] `gg impact src/file.ts` — bağımlılık analizi
- [ ] Git post-commit hook (otomatik index)
- [ ] Git pre-push hook (check)
- [ ] `gg context "konu"` — Qdrant + Memgraph birleşik sorgu
- [ ] Task get çıktısına ilgili kararlar + etkilenen dosyalar ekle

**Deliverable:** Agent'lar kod yapısını biliyor, impact analizi yapabiliyor.

## Faz 3 — Polish + Open Source (3-5 gün)

- [ ] README.md — hero banner, quick start, demo GIF
- [ ] Agent inject script — RULES.md'yi CLAUDE.md/.gsd/PROJECT.md'ye ekle
- [ ] `gg doctor` — bağlantı kontrolü, agent rules kontrolü
- [ ] Testler
- [ ] GitHub Actions CI
- [ ] GitHub release + `go install github.com/gurkan/gg@latest` desteği
- [ ] docs/ — writing adapters, RULES.md customization

**Toplam: ~2.5-3 hafta**

---

## Örnek Akış

```
Terminal 1: BMAD/Claude Code (Architect rolünde)
─────────────────────────────────────────────────
Sen: "Auth sistemi nasıl yapalım?"

Agent: (gg search "auth" → sonuç yok)
       "Auth ile ilgili daha önce bir karar yok.
        İki seçenek: JWT veya session-based..."

Sen: "JWT olsun"

Agent: (gg decide "JWT based auth" --reason "stateless, mobile uyumlu" --tags "auth")
       (gg reject "session-based auth" --reason "stateful, scaling zorlaştırır")
       "Kaydettim. Auth endpoint'i için task açıyorum."
       (gg task create "JWT auth endpoint" --detail "login, register, refresh token.
        JWT 1 saat expire, refresh 7 gün." --priority high --tags "auth,api")
       "TASK-001 açıldı."
       (gg tell "developer" "JWT auth tasarımı onaylandı, detaylar TASK-001'de")


Terminal 2: GSD2 (Developer rolünde)
─────────────────────────────────────────────────
Sen: "Taskları çöz"

GSD2: (gg inbox → "JWT auth tasarımı onaylandı, detaylar TASK-001'de")
      (gg task list --status pending → TASK-001)
      (gg task get TASK-001 → detay + JWT kararı + reject bilgisi)
      (gg impact "src/auth/" → yeni modül, mevcut bağımlılık yok)

      → kodu yazar: src/auth/login.ts, src/auth/register.ts
      → test yazar: src/auth/__tests__/
      → commit atar
      → (post-commit hook: gg index --changed)

      (gg task done TASK-001 "JWT auth implemented. Login, register, refresh endpoints ready.")
      (gg tell "qa" "auth endpoint'leri hazır, rate limiting ve token expire edge case'leri test edilmeli")
      "TASK-001 tamamlandı."
```

---

## Gelecek (v2 fikirler — MVP sonrası)

- Web UI dashboard (opsiyonel — tasklar, kararlar, graph görselleştirme)
- Agent otomatik tetikleme (daemon mode)
- OneLift entegrasyonu (`lift install gg`)
- Plugin marketplace (custom embedding modelleri, graph indexer'lar)
- Team mode — birden fazla developer aynı brain'i kullanır
- Pro features — advanced analytics, agent scoring
