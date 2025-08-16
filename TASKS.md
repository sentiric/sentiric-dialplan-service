# 🗺️ Sentiric Dialplan Service - Görev Listesi

Bu belge, `dialplan-service`'in geliştirme yol haritasını ve önceliklerini tanımlar.

---

### Faz 1: Veritabanı Tabanlı Karar Mekanizması (Mevcut Durum)

Bu faz, servisin hafızadaki mock veriler yerine, PostgreSQL'deki kurallara göre dinamik kararlar verebilmesini hedefler.

-   [x] **Temel gRPC Sunucusu:** `ResolveDialplan` RPC'sini implemente eden sunucu.
-   [x] **PostgreSQL Entegrasyonu:** `inbound_routes` ve `dialplans` tablolarından veri okuma yeteneği.
-   [x] **User Service Entegrasyonu:** Arayanın kimliğini doğrulamak için `user-service`'e gRPC çağrısı yapma.
-   [x] **Koşullu Mantık:** Arayanın durumuna (kayıtlı, misafir) ve hattın durumuna (bakım modu) göre farklı dialplan'ler döndürme.
-   [x] **Failsafe Mantığı:** Herhangi bir hata durumunda veya kural bulunamadığında, varsayılan olarak `DP_SYSTEM_FAILSAFE` planına yönlendirme.

---

### Faz 2: Gelişmiş Kural Mantığı ve Yönetim (Sıradaki Öncelik)

Bu faz, dialplan'in daha karmaşık ve dinamik kuralları desteklemesini ve yönetilebilir olmasını hedefler.

-   [ ] **Görev ID: DP-001 - CRUD gRPC Endpoint'leri**
    -   **Açıklama:** `dashboard-ui`'nin `dialplans` ve `inbound_routes` tablolarını yönetebilmesi için `CreateDialplan`, `UpdateInboundRoute` gibi CRUD operasyonlarını destekleyen yeni gRPC endpoint'leri ekle.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: DP-002 - Zamana Dayalı Yönlendirme**
    -   **Açıklama:** `inbound_routes` tablosuna `working_hours_dialplan_id` ve `off_hours_dialplan_id` gibi alanlar ekleyerek, çağrının geldiği saate göre farklı planların tetiklenmesini sağla.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: DP-003 - Tatil Takvimi Entegrasyonu**
    -   **Açıklama:** Resmi tatil günlerinde otomatik olarak "tatil anonsu" dialplan'ini tetikleyecek bir mantık ekle. Bu, `connectors-service` üzerinden bir takvim API'si ile entegre olabilir.
    -   **Durum:** ⬜ Planlandı.