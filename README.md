# 🗺️ Sentiric Dialplan Service

[![Status](https://img.shields.io/badge/status-active-success.svg)]()
[![Language](https://img.shields.io/badge/language-Go-blue.svg)]()
[![Protocol](https://img.shields.io/badge/protocol-gRPC_(mTLS)-green.svg)]()

**Sentiric Dialplan Service**, Sentiric platformu için dinamik çağrı yönlendirme kararları veren **stratejik karar merkezidir.** Yüksek performans ve basitlik için **Go** ile yazılmıştır. Gelen bir çağrının, o anki koşullara göre hangi iş akışını tetiklemesi gerektiğini belirleyen merkezi bir kural motoru (rule engine) olarak görev yapar.

## 🎯 Temel Sorumluluklar

*   **Dinamik Yönlendirme Mantığı:** Gelen bir çağrının hedef numarasına (`destination_number`) göre veritabanındaki `inbound_routes` tablosunu sorgular.
*   **Kullanıcı Tanıma:** Arayan numarayı (`caller_contact_value`) kullanarak `user-service`'e danışır ve arayanın kim olduğunu tespit eder.
*   **Koşullu Karar Verme:**
    *   Eğer aranan numara bakım modundaysa, **bakım anonsu** planını döndürür.
    *   Eğer arayan kişi sistemde kayıtlı değilse, **misafir karşılama** (`PROCESS_GUEST_CALL`) planını döndürür.
    *   Eğer arayan kayıtlı bir kullanıcı ise, o numaraya atanmış olan **aktif iş akışı** planını (`START_AI_CONVERSATION` vb.) döndürür.
*   **gRPC Arayüzü:** `ResolveDialplan` adında tek bir gRPC endpoint'i sunarak, `sip-signaling-service` gibi servislerin senkron ve tip-güvenli bir şekilde yönlendirme kararı almasını sağlar.

## 🛠️ Teknoloji Yığını

*   **Dil:** Go
*   **Servisler Arası İletişim:** gRPC (mTLS ile güvenli hale getirilmiş)
*   **Veritabanı Erişimi:** PostgreSQL (`pgx` kütüphanesi)
*   **Loglama:** `zerolog` ile yapılandırılmış, ortama duyarlı loglama.

## 🔌 API Etkileşimleri

*   **Gelen (Sunucu):**
    *   `sentiric-sip-signaling-service` (gRPC)
*   **Giden (İstemci):**
    *   `sentiric-user-service` (gRPC): Arayan kullanıcıyı doğrulamak için.
    *   `PostgreSQL`: Yönlendirme kurallarını ve dialplan detaylarını okumak için.

## 🚀 Yerel Geliştirme

1.  **Bağımlılıkları Yükleyin:**
2.  **Ortam Değişkenlerini Ayarlayın:** `.env.example` dosyasını `.env` olarak kopyalayın ve gerekli değişkenleri doldurun.
3.  **Servisi Çalıştırın:**

## 🤝 Katkıda Bulunma

Katkılarınızı bekliyoruz! Lütfen projenin ana [Sentiric Governance](https://github.com/sentiric/sentiric-governance) reposundaki kodlama standartlarına ve katkıda bulunma rehberine göz atın.

---
## 🏛️ Anayasal Konum

Bu servis, [Sentiric Anayasası'nın (v11.0)](https://github.com/sentiric/sentiric-governance/blob/main/docs/blueprint/Architecture-Overview.md) **Zeka & Orkestrasyon Katmanı**'nda yer alan merkezi bir bileşendir.