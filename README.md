# iSeat 路 瓒呮槦鍥句功棣嗗骇浣嶈嚜鍔ㄩ绾︾郴缁?
> 瓒呮槦锛堝涔犻€氾級鑷範瀹ゅ骇浣嶈嚜鍔ㄩ绾﹁剼鏈?/ 绯荤粺锛氳嚜鍔ㄦ姠搴с€佹粦鍧楅獙璇佽嚜鍔ㄧ牬瑙ｃ€佽嚜鍔ㄧ鍒般€佺画绾﹀埌闂銆佸叡浜簩缁寸爜搴撱€?> 鏀寔 **CLI 鍛戒护琛?* 涓?**Web 绠＄悊绯荤粺** 涓ょ褰㈡€侊紝Docker 涓€閿儴缃层€?
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## 鉁?鍔熻兘鐗规€?
| 鍔熻兘 | 璇存槑 |
| --- | --- |
| 馃幆 鑷姩鎶㈠骇 | 鍒扮偣鑷姩楂橀閲嶈瘯锛宔nc 绛惧悕 + 婊戝潡楠岃瘉鐮佸叏鑷姩鐮磋В |
| 馃攼 婊戝潡楠岃瘉鐮?| 閫嗗悜 captcha.chaoxing.com 鍗忚锛歝onf鈫掗獙璇佸浘鈫掑浘鍍忕己鍙ｅ尮閰?NCC)鈫抍heck鈫抳alidate锛岀函 Go 瀹炵幇 |
| 馃摑 鑷姩绛惧埌 | 棰勭害鐢熸晥绐楀彛鑷姩璋冪敤绛惧埌鎺ュ彛锛堟棤闇€鎵浘锛岄厤缃嵆鍙級 |
| 馃攣 鑷姩缁害 | 姣忔 4 灏忔椂鑷姩琛旀帴锛?*閬嶅巻姣忎釜鑷範瀹ょ湡瀹炵殑闂鏃堕棿**锛堢壒娈婂紑鏀炬椂闂翠紭鍏堬級锛屽崰搴у埌闂 |
| 馃搮 璺ㄥぉ寰幆 | 浠婃棩鍗犳瘮鍒伴棴棣嗗悗鑷姩绾︽槑鏃ワ紝澶╁ぉ寰幆锛圵eb 浠诲姟寮曟搸锛?|
| 馃彨 鑷範瀹ゅ垪琛?| 鑷姩鎷夊彇鍏ㄩ儴鑷範瀹ゅ強鍏跺悇鑷紑鏀?闂鏃堕棿 |
| 馃獞 搴т綅缃戞牸 | 浜?鍙€夈€佹殫=鍗犵敤鐨勫彲瑙嗗寲鏂瑰潡缃戞牸锛岀偣閫変唬鏇挎墜杈?|
| 馃摎 鍏变韩浜岀淮鐮佸簱 | 涓婁紶鐨勬瀛愪簩缁寸爜鑷姩鍏ュ簱锛屽叏鍛樺厤鎷嶇収鐩存帴璋冪敤 |
| 鈿?蹇€熼绾?| 棰勭害杩囩殑妗屽瓙涓€閿画绾︼紙浠婂ぉ/鏄庡ぉ/姣忓ぉ锛?|
| 馃梻 浠诲姟绠＄悊 | 鏆傚仠/鎭㈠/鍒犻櫎浠诲姟锛屾墜鍔ㄥ彇娑?閫€搴?|
| 馃捇 CLI / 馃寪 Web | 鍛戒护琛屽伐鍏?+ Vue3 Web 绠＄悊绯荤粺 |

## 馃П 鎶€鏈爤

- **鍚庣**锛欸o + Gin锛圧EST API + 闈欐€佹墭绠?+ 浠诲姟璋冨害鍣級
- **鍓嶇**锛歏ue 3 + TypeScript + Vite
- **鏁版嵁搴?*锛歁ySQL锛堢敓浜э級 / SQLite锛堟湰鍦板厹搴曪紝绾?Go 椹卞姩锛?- **閮ㄧ讲**锛欴ocker / docker-compose

```
server/            Go 鍚庣
  鈹溾攢 cxclient.go   瓒呮槦鍏ㄥ鎺ュ彛锛堢櫥褰?AES鍔犲瘑/enc绛惧悕/鎶㈠骇/绛惧埌/閫€搴?鎴块棿鍒楄〃/闂閬嶅巻锛?  鈹溾攢 captcha.go    婊戝潡楠岃瘉鐮佽嚜鍔ㄦ眰瑙ｅ櫒锛圢CC 缂哄彛鍖归厤锛?  鈹溾攢 qrdecode.go   妗岄潰浜岀淮鐮佽瘑鍒紙gozxing 绾?Go锛?  鈹溾攢 scheduler.go  浠诲姟寮曟搸锛氭姠搴р啋绛惧埌鈫掔画绾︹啋璺ㄥぉ寰幆锛?0s 鎵弿锛?  鈹斺攢 handlers.go   REST API
web/               Vue3+TS 鍓嶇锛堢櫧鑹插渾瑙掔畝娲?UI锛?  鈹溾攢 Login.vue     瓒呮槦璐﹀彿鐧诲綍
  鈹斺攢 Dashboard.vue 鍥涘ぇ鍔熻兘鍗＄墖 + 鍏变韩浜岀淮鐮佸簱 + 浠诲姟绠＄悊
schema.sql         MySQL 寤哄簱鑴氭湰
Dockerfile         澶氶樁娈垫瀯寤猴紙鍓嶇+鍚庣+杩愯闀滃儚锛?docker-compose.yml MySQL + Web 涓€閿儴缃?```

---

## 馃殌 蹇€熷紑濮?
### 鏂瑰紡涓€锛欴ocker Compose锛堟帹鑽愮敓浜э級

```bash
docker compose up -d --build
# 鎵撳紑 http://localhost:5251
# 璇峰厛淇敼 docker-compose.yml 涓殑 MYSQL_ROOT_PASSWORD 涓?MYSQL_DSN 鍐呭瘑鐮?```

### 鏂瑰紡浜岋細鏈湴杩愯

```bash
# 1. 鍚庣锛坰erver/ 鐩綍锛涙棤 MySQL 鏃惰嚜鍔ㄧ敤 SQLite 鍏滃簳锛?cd server
go build -o seatbook.exe .
./seatbook.exe                        # http://localhost:5251

# 2. 鍓嶇鏋勫缓锛坵eb/ 鐩綍锛屾瀯寤轰骇鐗╃敱 Go 鑷姩鎵樼锛?cd web
npm install
npm run build
```

### 鏂瑰紡涓夛細CLI 鍛戒护琛岋紙鎶㈠骇/绛惧埌鍗曟満鐗堬級

```bash
go build -o booking.exe .
./booking.exe check    -c config.json   # 杩為€氭€ф鏌?./booking.exe book     -c config.json   # 绔嬪嵆鎶㈠骇锛堣嚜鍔ㄨВ婊戝潡锛?./booking.exe sign     -c config.json   # 鑷姩绛惧埌
./booking.exe renew    -c config.json   # 缁害涓嬩竴鏃舵锛堜粖鏃ユ弧鑷姩绾︽槑鏃ワ級
./booking.exe watch    -c config.json   # 鎸佺画瀹堟姢锛氭姠搴р啋绛惧埌鈫掔画绾︹啋闂
./booking.exe qr       浜岀淮鐮?jpg        # 璇嗗埆妗屼笂浜岀淮鐮?```

## 馃枼 Web 绯荤粺鍥涘ぇ鍔熻兘

1. **鎵嬪姩閫夋嫨鍏朵粬搴т綅**锛氳嚜涔犲涓嬫媺锛?4涓紝鍚悇鑷棴棣嗘椂闂达級鈫?浠婂ぉ/鏄庡ぉ鍒囨崲 鈫?**搴т綅鏂瑰潡缃戞牸**锛堜寒=鍙€夛級鈫?棰勭害妯″紡锛堜粖澶?鏄庡ぉ/姣忓ぉ锛夆啋 纭棰勭害
2. **涓婁紶妗屽瓙浜岀淮鐮?*锛氭媿鐓т笂浼?鈫?鑷姩璇嗗埆 鈫?鍗犲骇鍒伴棴棣嗗苟姣忔棩寰幆锛涗笂浼犲嵆鍏?*鍏变韩浜岀淮鐮佸簱**锛屽叏鍛樺彲鐩存帴璋冪敤
3. **蹇€熼绾?*锛氬睍绀洪绾﹁繃鐨勬瀛?鈫?涓€閿画绾︿粖澶?鏄庡ぉ/姣忓ぉ
4. **浠诲姟绠＄悊**锛氭殏鍋?鎭㈠/鍒犻櫎浠诲姟锛涘綋鍓嶉绾﹀彇娑?閫€搴?
## 鈿欙笍 閰嶇疆

鐜鍙橀噺锛圵eb 鍚庣锛夛細

| 鍙橀噺 | 榛樿 | 璇存槑 |
| --- | --- | --- |
| `PORT` | 8080 | 鏈嶅姟绔彛 |
| `MYSQL_DSN` | 绌?| MySQL 杩炴帴涓诧紱绌哄垯鏈湴 SQLite |
| `SQLITE_PATH` | seatbook.db | SQLite 鏂囦欢璺緞 |
| `CX_LOGIN_URL` | passport2 fanyalogin | 瓒呮槦鐧诲綍鎺ュ彛 |
| `CX_BASE` | office.chaoxing.com | 搴т綅绯荤粺鍩熷悕 |
| `CX_SEAT_ID` | 105 | 鏈牎鍖哄骇浣嶄笟鍔?ID |
| `WEB_DIR` | ../web/dist | 鍓嶇闈欐€佺洰褰?|

## 馃梽 鏁版嵁搴?
- 閮ㄧ讲鐢?MySQL锛歚schema.sql` 寤哄簱锛圙ORM 鑷姩杩佺Щ琛ㄧ粨鏋勶級
- 琛細`users`锛堢櫥褰曠敤鎴凤紝瀵嗙爜 AES 鍔犲瘑锛夈€乣session_tokens`銆乣tasks`锛堝崰搴т换鍔★級銆乣qr_codes`锛堝叡浜簩缁寸爜搴擄級

## 馃攲 鏍稿績鎺ュ彛锛堥€嗗悜瑕佺偣锛?
- 鐧诲綍锛歚POST /fanyalogin`锛圓ES-128-CBC `u2oh6Vu^HWe4_AES`锛?- 鎶㈠骇锛歚POST /data/apps/seatengine/submit`锛宍enc = MD5(鍙傛暟鎸夊瓧姣嶅簭 [k=v] + [submit_enc])`
- 婊戝潡锛歚captcha/get/conf 鈫?captcha/get/verification/image 鈫?鍥惧儚NCC鍖归厤 鈫?captcha/check/verification/result 鈫?validate`
- 绛惧埌锛歚POST /data/apps/seatengine/sign {id, seatId, roomId}`
- 闂鏃堕棿锛歚room/info` 鐨?`seatEngineSpecialTime`锛堟寜鏄熸湡锛? `openTimeLongSettingJson` > `commonTimeConfig`

---

## 鈿狅笍 鍏嶈矗澹版槑

> 鏈郴缁熶粎鐢ㄤ簬**鏈汉璐﹀彿**鐨勫骇浣嶉绾﹁嚜鍔ㄥ寲鎿嶄綔锛岃鍦?*閬靛畧鎵€鍦ㄥ鏍″骇浣嶉绾﹁鍒?*鐨勫墠鎻愪笅浣跨敤銆?> 鍥犱娇鐢ㄦ湰宸ュ叿浜х敓鐨勮繚绾︺€侀鎺ф垨鍏朵粬鍚庢灉鐢变娇鐢ㄨ€呰嚜琛屾壙鎷呫€?> 鏈」鐩负寮€婧愬涔犲伐鍏凤紝浠ｇ爜浠呬緵瀛︿範浜ゆ祦銆?
## 馃摦 鑱旂郴鎴戜滑

- GitHub锛歔https://github.com/qvoo/iSeat](https://github.com/qvoo/iSeat)
- Bug / 鍔熻兘寤鸿锛氭杩庡湪浠撳簱鎻愪氦 **Issue**

## 馃搫 License

MIT
