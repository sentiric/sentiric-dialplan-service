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

- [ ] **Görev ID: DP-004 - Otomatik Inbound Route Oluşturma (Auto-Provisioning)**
    -   **Açıklama:** Eğer aranan bir numara `inbound_routes` tablosunda bulunamazsa, bu çağrıyı reddetmek yerine, o numara için otomatik olarak yeni bir `inbound_route` kaydı oluştur. Bu yeni kural, varsayılan olarak "sistem" tenant'ına ve `DP_GUEST_ENTRY` dialplan'ine atanmalıdır.
    -   **Kabul Kriterleri:**
        -   [ ] Veritabanında olmayan bir numaraya çağrı geldiğinde, `dialplan-service` loglarında "Yeni inbound route tespit edildi ve oluşturuldu" gibi bir mesaj görünmeli.
        -   [ ] Çağrı, `DP_SYSTEM_FAILSAFE` yerine `DP_GUEST_ENTRY` planı ile devam etmeli.
        -   [ ] Yöneticiye, yeni bir numaranın sisteme eklendiğine dair bir bildirim (gelecekte) gönderilmelidir.
        
---

### **FAZ 2: Platformun Yönetilebilir Hale Getirilmesi (Sıradaki Öncelik)**

**Amaç:** `dashboard-ui` gibi yönetim araçlarının, çağrı yönlendirme kurallarını tam olarak yönetebilmesini sağlamak.

-   [ ] **Görev ID: DP-001 - CRUD gRPC Endpoint'leri**
    -   **Açıklama:** `dialplans` ve `inbound_routes` tablolarını yönetmek için tam CRUD (Create, Read, Update, Delete) yeteneği sağlayan gRPC endpoint'leri oluştur.
    -   **Kabul Kriterleri:**
        -   [ ] `sentiric-contracts`'e `CreateDialplan`, `UpdateDialplan`, `DeleteDialplan`, `ListDialplans` RPC'leri eklenmeli.
        -   [ ] `sentiric-contracts`'e `CreateInboundRoute`, `UpdateInboundRoute`, `DeleteInboundRoute`, `ListInboundRoutes` RPC'leri eklenmeli.
        -   [ ] `dialplan-service`, bu 10 yeni RPC'yi veritabanı işlemleriyle birlikte tam olarak implemente etmeli.

---

### **FAZ 3: Akıllı ve Dinamik Yönlendirme**

**Amaç:** Çağrı yönlendirme kararlarını statik kuralların ötesine taşıyarak daha dinamik ve "akıllı" hale getirmek.

-   [ ] **Görev ID: DP-002 - Zamana Dayalı Yönlendirme (Mesai Saatleri)**
    -   **Açıklama:** Çağrının geldiği saate ve güne göre farklı planların tetiklenmesini sağla.
    -   **Kabul Kriterleri:**
        -   [ ] `inbound_routes` tablosuna `off_hours_dialplan_id` alanı eklenmeli.
        -   [ ] `tenants` tablosuna `working_hours` (örn: "Mon-Fri 09:00-18:00") ve `timezone` alanları eklenmeli.
        -   [ ] `ResolveDialplan` mantığı, çağrı zamanını kiracının zaman dilimine göre kontrol ederek `active_dialplan_id` veya `off_hours_dialplan_id` arasında seçim yapmalı.

-   [ ] **Görev ID: DP-003 - Harici Veriye Dayalı Yönlendirme (Tatil Takvimi)**
    -   **Açıklama:** Resmi tatil günlerinde otomatik olarak "tatil anonsu" dialplan'ini tetikleyecek bir mantık ekle.
    -   **Kabul Kriterleri:**
        -   [ ] `dialplan-service`, `connectors-service` (henüz yok) veya harici bir takvim API'si ile entegre olabilmeli.
        -   [ ] Çağrı geldiğinde, o günün tatil olup olmadığını kontrol etmeli ve eğer tatilse özel bir `holiday_dialplan_id`'yi tetiklemeli.