### **`sentiric-dialplan-service/TASKS.md` (Güncellenmiş Hali)**

# 🗺️ Sentiric Dialplan Service - Geliştirme Yol Haritası (v4.0)

Bu belge, `dialplan-service`'in geliştirme görevlerini projenin genel fazlarına uygun olarak listeler.

---

### **Gelecek Fazlar: Akıllı ve Dinamik Yönlendirme**

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