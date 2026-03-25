const puppeteer = require('puppeteer');
const path = require('path');

(async () => {
  const browser = await puppeteer.launch({ headless: "new" });
  const page = await browser.newPage();

  page.on('console', msg => {
    if (msg.text().includes('original')) {
      console.log('Benchmark result:', msg.text());
    }
  });

  await page.goto(`file://${path.resolve(__dirname, 'benchmark4.html')}`);
  await page.waitForSelector('#result');
  await browser.close();
})();
