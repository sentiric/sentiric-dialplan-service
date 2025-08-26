# 🗺️ Sentiric Dialplan Service - Mantık ve Akış Mimarisi

**Belge Amacı:** Bu doküman, `dialplan-service`'in Sentiric platformunun **stratejik karar merkezi** olarak rolünü, bir çağrıya nasıl bir "plan" atadığını ve bu kararı verirken hangi servislerle nasıl etkileşimde bulunduğunu açıklar.

---

## 1. Stratejik Rol: "Akıllı Çağrı Trafik Polisi"

Bu servisin tek ve en önemli görevi, gelen bir çağrıya "Şimdi ne yapmalıyız?" sorusuna anında ve doğru bir cevap vermektir. Çağrı akışının ilk ve en kritik kararını bu servis alır.

**Bu servis sayesinde platform:**
1.  **Dinamik Olur:** Çağrıların nasıl ele alınacağı kodun içine gömülü değildir. Veritabanındaki basit kurallarla, bir telefon numarasının davranışını anında değiştirebilirsiniz (örn: mesai dışı saatlerde sesli mesaja yönlendirme).
2.  **Bağlama Duyarlı Olur:** Karar verirken sadece aranan numarayı değil, aynı zamanda arayanın kim olduğunu (`user-service`'ten gelen bilgi) ve hattın mevcut durumunu (bakım modu vb.) dikkate alır.
3.  **Sorumlulukları Ayrıştırır:** `sip-signaling-service` sadece "postacı" görevi görürken, `agent-service` sadece "uygulayıcı" olur. Karar verme yükü tamamen bu servistedir.

---

## 2. Temel Çalışma Prensibi: Kural Tabanlı Karar Motoru

Servis, `sip-signaling`'den bir `ResolveDialplan` isteği aldığında, bir dizi kuralı sırayla uygular:

1.  **Hat Sorgulama:** Aranan numarayı (`destination_number`) veritabanındaki `inbound_routes` tablosunda arar.
2.  **Durum Kontrolü:** Bulunan hattın `is_maintenance_mode` gibi özel durumlarını kontrol eder.
3.  **Arayan Sorgulama:** Arayan numarayı (`caller_contact_value`) `user-service`'e sorarak kimliğini tespit eder.
4.  **Kural Eşleştirme:** Bu üç bilgiyi (Hat, Durum, Arayan) birleştirerek en uygun `dialplan_id`'yi seçer.
5.  **Planı Getirme:** Seçilen `dialplan_id`'nin detaylarını (`action`, `action_data`) `dialplans` tablosundan okur.
6.  **Yanıt Döndürme:** Tüm bu bilgileri içeren zengin bir `ResolveDialplanResponse` mesajını `sip-signaling`'e geri döner.

---

## 3. Uçtan Uca Karar Akışı: Bir Çağrının Kaderi

Bir çağrı geldiğinde, `dialplan-service`'in karar verme süreci şöyledir:

```mermaid
sequenceDiagram
    participant Signaling as SIP Signaling
    participant Dialplan as Dialplan Service
    participant UserService as User Service
    participant PostgreSQL

    Signaling->>Dialplan: ResolveDialplan(arayan="90555...", aranan="90212...")
    
    Dialplan->>PostgreSQL: SELECT * FROM inbound_routes WHERE phone_number='90212...'
    PostgreSQL-->>Dialplan: Hat bilgisi (active_plan: 'DP_WELCOME', failsafe_plan: 'DP_MAINTENANCE')

    Note right of Dialplan: Hat bakım modunda değil, devam et.
    
    Dialplan->>UserService: FindUserByContact(contact_value="90555...")
    
    alt Arayan Bulundu
        UserService-->>Dialplan: User nesnesi (user_id, tenant_id)
        Note right of Dialplan: Kullanıcı tanınıyor, aktif plana yönlendir. <br> Seçilen Plan: 'DP_WELCOME'
    else Arayan Bulunamadı
        UserService-->>Dialplan: NOT_FOUND Hatası
        Note right of Dialplan: Misafir kullanıcı, misafir planına yönlendir. <br> Seçilen Plan: 'DP_GUEST_ENTRY'
    end

    Dialplan->>PostgreSQL: SELECT action, action_data FROM dialplans WHERE id='DP_WELCOME'
    PostgreSQL-->>Dialplan: action: 'START_AI_CONVERSATION', action_data: {...}

    Dialplan-->>Signaling: ResolveDialplanResponse (plan, kullanıcı, hat bilgileriyle dolu)
```