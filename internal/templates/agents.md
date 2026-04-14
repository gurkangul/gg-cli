# Agent Guidance

Bu proje paylaşılan bir bilgi tabanı kullanır: **gg** CLI.
Tüm kararlar, tasklar, agent-arası mesajlar ve reddedilen yaklaşımlar `gg` üzerinden kaydedilir.

Her agent kendi terminalinde bağımsız çalışır ama hepsi aynı Qdrant + Ollama altyapısına yazar.
Böylece bir agent'ın aldığı karar, diğer agent'ın da görebildiği ortak bellekte olur.

> Kullanıcı senden `gg` çalıştırmanı ASLA istemez — sen tespit ederek otomatik çalıştırırsın.

---

# GG KURALLARI

## OTURUM BAŞLANGIÇ

Her konuşma başladığında ilk iş:
```
gg status
```
Açık tasklar, bekleyen mesajlar, son kararlar özetini al ve kullanıcıya kısaca özetle.

## TARTIŞMA SIRASINDA

Kullanıcıyla bir konu tartışırken:

1. Konu hakkında daha önce karar alınmış mı:
   ```
   gg search "konu"
   ```
2. Reddedilmiş yaklaşım var mı kontrol et (aynı komut rejection'ları da döndürür)
3. Varsa kullanıcıya bildir — "daha önce X kararı alınmış, Y yaklaşımı reddedilmiş"

## KARAR ANI

Kullanıcı ile bir karara vardığında (açık veya üstü kapalı):

- "JWT kullanalım" → karar
- "tamam öyle yapalım" → önceki önerinin onayı = karar
- "evet mantıklı" → karar

Tespit ettiğinde:
```
gg decide "kısa karar" --reason "sebep" --tags "etiketler"
```
Kullanıcıya: "Karar olarak kaydettim."

## TASK OLUŞTURMA

Bir iş yapılması gerektiği netleştiğinde:
```
gg task create "başlık" --detail "açıklama" --priority high --tags "etiketler"
```
Kullanıcıya: "Task açtım: TASK-XXX"

## TASK ÇÖZME

Kullanıcı "taskları çöz" veya "TASK-XXX'i yap" dediğinde:

1. `gg task list --status pending`
2. Her task için:
   1. `gg task get TASK-XXX`
   2. Kodu yaz, test et, commit at
   3. `gg task done TASK-XXX "özet"`

## BAŞKA AGENT'A MESAJ

Bir iş başka role kalıyorsa (ör. architect kararı verdi, developer yazacak):
```
gg tell "developer" "mesaj" --from architect
```

Rol kendini belirlemek için: `export GG_ROLE=architect` (ya da developer, qa).

## HATA / BLOCKER

Task çözülemiyorsa:
```
gg task block TASK-XXX "sebep"
```

## REDDEDİLEN YAKLAŞIMLAR

Bir yaklaşım değerlendirildi ama seçilmedi — mutlaka kaydet:
```
gg reject "yaklaşım" --reason "neden"
```
Bu, başka agent'ların aynı reddedilen yolu tekrar önermesini engeller.

## ASLA YAPMA

- `gg` olmadan karar alma
- Reddedilmiş yaklaşımı tekrarlama (search önce)
- Task açmadan "sonra yaparız" deme
- Kullanıcıdan `gg` komutu çalıştırmasını isteme
