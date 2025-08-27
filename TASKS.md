### **`sentiric-dialplan-service/TASKS.md` (Güncellenmiş Hali)**

# 🗺️ Sentiric Dialplan Service - Geliştirme Yol Haritası (v4.0)

Bu belge, `dialplan-service`'in geliştirme görevlerini projenin genel fazlarına uygun olarak listeler.

---

### **FAZ 1: Temel Karar Mekanizması (Mevcut Durum)**

**Amaç:** Gelen bir çağrıya, arayanın kimliğine ve hattın durumuna göre temel bir yönlendirme kararı verebilmek.

-   [x] **Görev ID: DP-000A - Temel gRPC Sunucusu ve Veritabanı Entegrasyonu**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Kabul Kriterleri:** `ResolveDialplan` RPC'sini sunan mTLS'li bir gRPC sunucusu çalışır ve PostgreSQL'e bağlanır.

-   [x] **Görev ID: DP-000B - Koşullu Karar Mantığı**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Kabul Kriterleri:**
        -   [x] `inbound_routes` tablosundan aranan numaraya göre doğru kuralı bulur.
        -   [x] `user-service`'e danışarak arayanın "kayıtlı" mı "misafir" mi olduğunu anlar.
        -   [x] Hattın `is_maintenance_mode` bayrağını kontrol eder.
        -   [x] Bu koşullara göre doğru `dialplan_id`'yi seçer (`active_dialplan_id`, `DP_GUEST_ENTRY`, `failsafe_dialplan_id`).

-   [x] **Görev ID: DP-000C - Failsafe Mekanizması**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Kabul Kriterleri:** `user-service` veya PostgreSQL'den bir hata döndüğünde, akış kesilmez; bunun yerine loglama yapılır ve `failsafe_dialplan_id`'ye (veya nihai olarak `DP_SYSTEM_FAILSAFE`'e) yönlendirme yapılır.

-   [x] **Görev ID: DP-004 - Otomatik Inbound Route Oluşturma (Auto-Provisioning)**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Açıklama:** Eğer aranan bir numara `inbound_routes` tablosunda bulunamazsa, bu çağrıyı reddetmek yerine, o numara için otomatik olarak yeni bir `inbound_route` kaydı oluştur. Bu yeni kural, varsayılan olarak "sistem" tenant'ına ve `DP_GUEST_ENTRY` dialplan'ine atanmalıdır.
    -   **Kabul Kriterleri:**
        -   [x] Veritabanında olmayan bir numaraya çağrı geldiğinde, `dialplan-service` loglarında "Yeni inbound route tespit edildi ve oluşturuldu" gibi bir mesaj görünmeli.
        -   [x] Çağrı, `DP_SYSTEM_FAILSAFE` yerine `DP_GUEST_ENTRY` planı ile devam etmeli.
        -   [ ] Yöneticiye, yeni bir numaranın sisteme eklendiğine dair bir bildirim (gelecekte) gönderilmelidir.
        
---

### **FAZ 2: Platformun Yönetilebilir Hale Getirilmesi (Sıradaki Öncelik)**

**Amaç:** `dashboard-ui` gibi yönetim araçlarının, çağrı yönlendirme kurallarını tam olarak yönetebilmesini sağlamak.

-   [x] **Görev ID: DP-001 - CRUD gRPC Endpoint'leri**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Açıklama:** `dialplans` ve `inbound_routes` tablolarını yönetmek için tam CRUD (Create, Read, Update, Delete) yeteneği sağlayan gRPC endpoint'leri oluştur.
    -   **Kabul Kriterleri:**
        -   [x] `sentiric-contracts`'e `CreateDialplan`, `UpdateDialplan`, `DeleteDialplan`, `ListDialplans` RPC'leri eklenmeli.
        -   [x] `sentiric-contracts`'e `CreateInboundRoute`, `UpdateInboundRoute`, `DeleteInboundRoute`, `ListInboundRoutes` RPC'leri eklenmeli.
        -   [x] `dialplan-service`, bu 10 yeni RPC'yi veritabanı işlemleriyle birlikte tam olarak implemente etmeli.

---

### **FAZ 2.5: Anayasal Uyum ve Yeniden Yapılandırma**

**Amaç:** Projenin kod tabanını, `sentiric-governance` anayasasında tanımlanan en yüksek sürdürülebilirlik, test edilebilirlik ve gözlemlenebilirlik standartlarına getirmek.

-   [x] **Görev ID: DP-005 - Anayasa Uyumlu Katmanlı Mimariye Geçiş**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Açıklama:** `main.go`'daki tüm mantığı, projenin "Genesis Bloğu" ve "Gözlemlenebilirlik Standardı" ilkelerine tam uyumlu olarak ayrı katmanlara (repository, service, server) taşımak.
    -   **Kabul Kriterleri:**
        -   [x] **Repository Katmanı (`internal/repository`):** Tüm veritabanı sorguları bu katmana taşınmalı ve bir `interface` arkasına soyutlanmalıdır.
        -   [x] **Servis Katmanı (`internal/service`):** Tüm iş mantığı bu katmana taşınmalı ve sadece `repository` interface'ine bağımlı olmalıdır.
        -   [x] **Sunucu Katmanı (`internal/server`):** gRPC handler'ları bu katmana taşınmalı ve sadece `service` katmanını çağıran "ince" (thin) bir katman olmalıdır.
        -   [x] **Gözlemlenebilirlik Entegrasyonu:**
            -   [x] Tüm loglara `trace_id` gibi standart alanları otomatik ekleyen bir yapı kurulmalıdır.
            -   [x] `OBSERVABILITY_STANDARD.md`'ye uygun olarak Prometheus için bir `/metrics` endpoint'i eklenmelidir.
        -   [x] **Ana Paket (`main.go`):** Sadece bağımlılıkları başlatan ve katmanları birbirine bağlayan bir "kablolama" (wiring) noktası haline gelmelidir.
---

### **FAZ 3: Akıllı ve Dinamik Yönlendirme**

**Amaç:** Çağrı yönlendirme kararlarını statik kuralların ötesine taşıyarak daha dinamik ve "akıllı" hale getirmek.

-   [ ] **Görev ID: DP-002 - Zamana Dayalı Yönlendirme (Mesai Saatleri)**
    -   **Durum:** ⬜ **Planlandı**
    -   **Açıklama:** Çağrının geldiği saate ve güne göre farklı planların tetiklenmesini sağla.
    -   **Kabul Kriterleri:**
        -   [ ] `inbound_routes` tablosuna `off_hours_dialplan_id` alanı eklenmeli.
        -   [ ] `tenants` tablosuna `working_hours` (örn: "Mon-Fri 09:00-18:00") ve `timezone` alanları eklenmeli.
        -   [ ] `ResolveDialplan` mantığı, çağrı zamanını kiracının zaman dilimine göre kontrol ederek `active_dialplan_id` veya `off_hours_dialplan_id` arasında seçim yapmalı.

-   [ ] **Görev ID: DP-003 - Harici Veriye Dayalı Yönlendirme (Tatil Takvimi)**
    -   **Durum:** ⬜ **Planlandı**
    -   **Açıklama:** Resmi tatil günlerinde otomatik olarak "tatil anonsu" dialplan'ini tetikleyecek bir mantık ekle.
    -   **Kabul Kriterleri:**
        -   [ ] `dialplan-service`, `connectors-service` (henüz yok) veya harici bir takvim API'si ile entegre olabilmeli.
        -   [ ] Çağrı geldiğinde, o günün tatil olup olmadığını kontrol etmeli ve eğer tatilse özel bir `holiday_dialplan_id`'yi tetiklemeli.