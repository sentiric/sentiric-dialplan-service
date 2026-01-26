# 🗺️ Sentiric Dialplan Service - Mantık Mimarisi (Final)

**Rol:** Karar Mercii. Çağrının kaderini belirleyen kurallar motoru.

## 1. Karar Algoritması (Rule Engine)

Bir çağrı geldiğinde (`ResolveDialplan`), servis şu sırayla karar verir:

1.  **Sistem Kontrolü:**
    *   Numara "Bakım Modu"nda mı? -> `PLAY_ANNOUNCEMENT (Maintenance)`

2.  **Kullanıcı Tanıma (Identification):**
    *   `user-service`'e sor: "Bu numarayı (Arayan) tanıyor muyuz?"
    *   **Tanınmıyor:** -> `PROCESS_GUEST_CALL` (Misafir Karşılama)
    *   **Tanınıyor:** -> Kullanıcı verisini (Ad, TenantID) hafızaya al.

3.  **Hedef Analizi (Routing):**
    *   Aranan numara bir **Dahili Abone** mi? -> `BRIDGE_CALL`
    *   Aranan numara bir **Sistem Hattı** mı? -> `START_AI_CONVERSATION`

## 2. Aksiyon Sözlüğü (Action Dictionary)

Proxy ve Agent bu aksiyonlara göre hareket eder:

| Aksiyon | Anlamı | Hedef Servis |
| :--- | :--- | :--- |
| **`BRIDGE_CALL`** | P2P bağlantı kur. | `registrar-service` |
| **`START_AI_CONVERSATION`** | Standart AI asistanı başlat. | `b2bua` -> `agent` |
| **`PROCESS_GUEST_CALL`** | KVKK/Aydınlatma metni ile başlat. | `b2bua` -> `agent` |
| **`PLAY_ANNOUNCEMENT`** | Sadece ses çal ve kapat. | `b2bua` -> `media` |

---
