# GG KURALLARI

Sen bu projede paylaşılan bir bilgi tabanı ile çalışıyorsun.
Tüm kararlar, tasklar ve bilgi alışverişi gg CLI üzerinden yapılır.
Kullanıcı senden gg çalıştırmanı ASLA istemez — sen otomatik çalıştırırsın.

## OTURUM BAŞLANGIÇ

Her konuşma başladığında ilk iş:
```
gg status
```
Açık tasklar, bekleyen mesajlar, son kararlar özetini al ve kullanıcıya bildir.

## TARTIŞMA SIRASINDA

Kullanıcıyla bir konu tartışırken:

1. Konu hakkında daha önce karar alınmış mı:
   ```
   gg search "konu"
   ```
2. Reddedilmiş yaklaşım var mı kontrol et
3. Varsa kullanıcıya bildir

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
   a. `gg task get TASK-XXX`
   b. `gg impact "etkilenecek dosyalar"`
   c. Kodu yaz, test et, commit at
   d. `gg task done TASK-XXX "özet"`

## DOSYA DEĞİŞTİRMEDEN ÖNCE

Her zaman:
```
gg impact src/dosya/yolu.ts
```

## BAŞKA AGENT'A MESAJ

Bir iş başka role kalıyorsa:
```
gg tell "hedef-rol" "mesaj"
```

## HATA / BLOCKER

Task çözülemiyorsa:
```
gg task block TASK-XXX "sebep"
```

## REDDEDİLEN YAKLAŞIMLAR

Yaklaşım reddedildiğinde:
```
gg reject "yaklaşım" --reason "neden"
```

## ASLA YAPMA

- gg olmadan karar alma
- Reddedilmiş yaklaşımı tekrarlama
- Task açmadan "sonra yaparız" deme
- impact kontrolü yapmadan dosya değiştirme
- Kullanıcıdan gg komutu çalıştırmasını isteme
