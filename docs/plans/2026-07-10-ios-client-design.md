# iOS クライアント設計（iPhone を Mac の外部ディスプレイにする）

- 日付: 2026-07-10
- ステータス: 設計承認済み（松崎さん 2026-07-10）
- 対象: iPhone のみ。iPad は Sidecar で運用するため対象外
- 前提設計判断（承認済み）: Approach A = iPhone インターネット共有の USB リンク（CDC-NCM）を使う

## 1. 目的とスコープ

USB 接続した iPhone を、Android クライアントと同じ仕組み（Mac 側 Go サーバー + 仮想ディスプレイ + H.264 ストリーミング + タッチ逆注入）で Mac の外部ディスプレイにする。

- **In scope**: iOS クライアントアプリ新規実装（Swift / SwiftUI）、既存 Go サーバーへの接続
- **Out of scope**: サーバー側のコード変更（ゼロ変更で成立する。§3）、iPad 対応、AirPlay/Sidecar 連携、ワイヤレス(Wi-Fi LAN)モードの iOS 対応（将来課題。v1 は USB リンク経由のみ）

## 2. トランスポート: iPhone インターネット共有 over USB（Approach A）

adb reverse に相当する仕組みが iOS に無いため、**iPhone のインターネット共有（Personal Hotspot）を USB 経由で有効にし、Mac–iPhone 間に CDC-NCM の実 IP リンクを張る**。

- Mac 側にネットワークサービス「iPhone USB」が現れ、Mac は 172.20.10.2〜172.20.10.14 の DHCP アドレスを取得。iPhone はゲートウェイ 172.20.10.1
- iPhone クライアントは **Mac の IP に直接ダイヤル**する（トンネル不要）
- 制約・注意:
  - キャリアのテザリング契約が必要（インターネット共有の有効化条件）
  - Mac のネットワークサービス順序で「iPhone USB」を下位に置く（インターネット経路が iPhone 経由に化けるのを防ぐ）
  - **ユーザー手順**: 設定 → インターネット共有 を ON（USB 接続中に）
- **未確定事項（実装前にライブ検証）**: CDC-NCM リンク上で mDNS(Bonjour) が通るか。通らない場合のフォールバックは §5

## 3. サーバー側: ゼロ変更で成立する根拠

iOS クライアントは ClientHello で `connectionType: "usb"` を名乗る。これにより既存の USB モードのパスがそのまま使える：

- `allocateClientPorts`（server/cmd/server/main.go:58-100）: `IsUSB()` なら `stream.NewTCPVideoServer(0)` を生成 → **TCP ビデオ**になる
- TCP ビデオサーバーは `net.Listen("tcp", ":0")` で**全インターフェース listen**（server/internal/stream/tcp.go:46）→ iPhone から 172.20.10.x 経由で直接ダイヤル可能
- タッチサーバーも全インターフェース listen（server/internal/touch/server.go:22）
- コントロールポートは **9000 固定**（server/cmd/server/main.go:28）
- 副作用: サーバーは USB クライアント接続時に adb reverse 設定を **接続中の Android 実機シリアル全部**に発行するが、iPhone 接続では無関係な Android 端末にポート forward が張られるだけで無害（検証済みの挙動）
- ServerHello 送信後、サーバーは `videoServer.AcceptOne()` でブロックする（main.go:272-288）→ **iOS は ServerHello 受信後すみやかに videoPort にダイヤルする必要がある**

UDP ビデオ（wifi モード）を使わない理由: USB リンクはロスレスなので UDP の再組立て（フラグメンテーション処理）の複雑さに見合う利点がない。新しい connectionType（"hotspot" 等）の追加も YAGNI で却下 — "usb" の再利用で完結する。

## 4. プロトコル契約（iOS 実装が従う正確な仕様）

### 4.1 コントロールチャネル（TCP :9000、改行区切り JSON）

ハンドシェイク: ClientHello → ServerHello → ClientReady。Codable ミラーは server/internal/protocol/messages.go と正確に一致させる：

```json
// ClientHello (iOS → Mac)
{"device": "iPhone 15 Pro", "screen": {"width": 2556, "height": 1179, "dpi": 460},
 "capabilities": ["touch"], "codecs": ["h264"], "connectionType": "usb", "bitrate": 8000000}

// ServerHello (Mac → iOS)
{"virtualDisplay": {"width": 2556, "height": 1179}, "codec": "h264", "bitrate": 8000000,
 "fps": 60, "streamPort": 5001, "touchPort": 49923, "videoPort": 49922}

// ClientReady (iOS → Mac) — USB モードでは udpPort=0 固定（TCP ビデオの合図）
{"udpPort": 0}
```

- JSON キーは messages.go の json タグどおり（`connectionType` / `virtualDisplay` / `udpPort` 等）
- `streamPort` は WiFi/UDP モード用のレガシー値で、サーバー全体のデフォルト 5001 が常に入る（control/server.go:43。main.go は SetStreamPort を呼ばない）。**USB モードの iOS クライアントは無視する**（ビデオは `videoPort`、タッチは `touchPort` を使う）
- Heartbeat 型は定義されているが現行未使用。切断検知はソケットクローズ（§7）

### 4.2 ビデオストリーム（TCP、length-prefix フレーミング）

ServerHello の `videoPort` にダイヤル。ワイヤフォーマット（server/internal/stream/tcp.go）:

```
[4B 全長 BE][17B ヘッダ][H.264 NAL ペイロード]
ヘッダ: Sequence u32 @0-3 | Timestamp u64 @4-11 (マイクロ秒) | FrameType u8 @12
       | FragIndex u16 @13-14 (常に0) | FragTotal u16 @15-16 (常に1)
FrameType: IDR=0x01, P=0x02, B=0x03, SPS=0x10, PPS=0x11 (stream/packet.go)
```

- ペイロードは **Annex-B 形式**（スタートコード 00 00 00 01。encoder_bridge.m が AVCC→Annex-B 変換済み）
- iOS 側は VideoToolbox デコードのため **Annex-B→AVCC 再変換**と、SPS/PPS からの `CMVideoFormatDescription` 生成が必要
- サーバーは非キーフレームの書き込みタイムアウト（150ms）を黙ってスキップする → シーケンス番号の欠番は正常。デコーダは次の IDR まで待つ設計にする

### 4.3 タッチストリーム（TCP、34 バイト固定長 BE）

ServerHello の `touchPort` にダイヤル。イベントレイアウト（server/internal/touch/event.go）:

```
Byte 0: Type u8 (finger=0, pen=1) | Byte 1: Action u8 (down=0, move=1, up=2)
Bytes 2-21: X, Y, Pressure, TiltX, TiltY (各 f32 BE)
Bytes 22-25: PointerID i32 | Bytes 26-33: Timestamp i64 (ミリ秒)
```

- **X/Y は 0..1 正規化座標**（server/internal/input/injector.go:17）。iOS 側はビュー座標を描画領域サイズで割って送る
- v1 からタッチ送信を含める（finger のみ。pen/tilt は 0 埋め）

## 5. ディスカバリ（3 段フォールバック）

1. **Bonjour**: `NWBrowser` で `_androidmac._tcp`（サーバーは grandcat/zeroconf で port 9000 を広告、server/internal/discovery/mdns.go）。CDC-NCM 上で通るかは未検証（§2）
2. **IP スキャン**: Bonjour 不成立時、172.20.10.2〜172.20.10.14 の 13 アドレス × port 9000 に短タイムアウトで並列プローブ（Mac の hotspot 側アドレスは必ずこのレンジ）
3. **手動 IP 入力**: 上記全滅時の最終手段として接続画面に IP 入力欄を置く

## 6. iOS アプリ構成（Swift / SwiftUI、新規プロジェクト `ios-client/`）

| コンポーネント | 責務 | 主要 API |
|---|---|---|
| `BonjourBrowser` | `_androidmac._tcp` の探索 + IP スキャンフォールバック | Network framework `NWBrowser` / `NWConnection` |
| `ControlChannel` | :9000 への TCP 接続、改行区切り JSON の送受、ハンドシェイク状態機械 | `NWConnection` + `Codable` |
| `VideoStream` | videoPort への TCP 接続、length-prefix パーサ（§4.2） | `NWConnection` |
| `H264Decoder` | Annex-B→AVCC 変換、SPS/PPS→FormatDescription、デコード | VideoToolbox `VTDecompressionSession` |
| `VideoRenderer` | デコード済みフレームの表示 | `AVSampleBufferDisplayLayer` |
| `TouchStreamer` | タッチ座標の正規化と 34B BE イベント送信（§4.3) | `NWConnection` |
| UI (SwiftUI) | 接続画面（探索結果リスト + 手動 IP）→ フルスクリーン表示画面 | `isIdleTimerDisabled = true`（スリープ防止） |

データフロー: 接続画面で探索 → ControlChannel がハンドシェイク → ClientReady(udpPort:0) 送信と**並行して即座に** videoPort / touchPort へダイヤル → フレーム受信 → デコード → 表示。タッチは表示画面から TouchStreamer へ。

## 7. エラーハンドリング / 切断

- 切断検知 = コントロールソケットのクローズ（サーバー側と同じ方式。Heartbeat 不使用）
- いずれかのソケットが切れたら全ソケットを閉じてクリーンアップ → 接続画面に戻り再接続 UI を表示
- デコードエラー（フォーマット未確立で P フレーム到着等）: 破棄して次の SPS/PPS/IDR を待つ

## 8. テスト戦略

- **ユニットテスト**: length-prefix パーサ（分割着信・複数フレーム結合着信）、JSON メッセージ codec、Annex-B→AVCC 変換、タッチイベントのシリアライズ（34B ゴールデンバイナリ照合）
- **統合テスト**: iOS シミュレータは Mac とネットワークを共有する → **シミュレータから localhost の Go サーバーに接続して E2E 検証**（hotspot 不要でパイプライン全体を回せる）
- **実機テスト**: hotspot リンク確立 → ディスカバリ 3 段の順に検証 → 表示・タッチ・切断復帰

## 9. コード署名 / 配布

- **v1 は無料 Apple ID の Personal Team 署名で開始**（7 日で期限切れ → Xcode から再署名が必要）
- 常用が確定したら Apple Developer Program（$99/年、1 年署名）に移行を検討

## 10. 未解決事項（実装前に潰す）

1. mDNS over CDC-NCM の可否（hotspot リンク確立後に即検証。不可でも §5 のフォールバックで成立）
2. ユーザー側準備: iPhone を USB 接続した状態で 設定 → インターネット共有 を ON にし、Mac 側に「iPhone USB」サービスと 172.20.10.x アドレスが現れることの確認
