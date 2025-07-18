const express = require('express');
const app = express();
const PORT = process.env.PORT || 3002;

// Kural defterimizi güncelliyoruz. Artık gerçek numaramızı tanıyor.
const dialplan = {
  '902124548590': [
    { action: 'log', data: 'Ana hat (902124548590) arandı. IVR uygulamasına yönlendiriliyor.' },
    { action: 'route', data: 'IVR_Giris' }
  ]
};

app.get('/dialplan/:destination', (req, res) => {
  const destination = req.params.destination;
  const plan = dialplan[destination];

  console.log(`[Dialplan Service] '${destination}' hedefi için yönlendirme planı sorgusu alındı.`);

  if (plan) {
    console.log(`--> ✅ Yönlendirme planı bulundu. Plan gönderiliyor.`);
    res.status(200).json(plan);
  } else {
    console.log(`--> ❌ Yönlendirme planı bulunamadı. 404 Not Found yanıtı gönderiliyor.`);
    res.status(404).json({ error: 'Dialplan not found' });
  }
});

app.listen(PORT, '0.0.0.0', () => {
  console.log(`✅ [Dialplan Service] Servis http://0.0.0.0:${PORT} adresinde dinlemede.`);
});