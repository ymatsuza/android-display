# 接続前画面の物理向き自動検知（反転チェックボックス自動化）

## Overview

`MainActivity`（接続前設定画面、画面自体はlandscape固定）で、端末の物理的な上下反転を重力センサーで検知し、既存の`orientationReverse`チェックボックスを自動でON/OFFする。手動タップでの上書きは常に可能なまま残す。

**決定済み事項**（ユーザー確認済み）:
- 自動化の対象は`orientationReverse`のみ。`orientationMode`（portrait/landscapeのラジオ選択）は自動化しない
- 自動検知後も手動でチェックボックスを上書き可能（自動検知＋手動オーバーライド可）

## 対象コード

- `client/app/src/main/java/com/androidmac/client/MainActivity.kt`
  - `orientationReverse: CheckBox`（既存フィールド）
  - `onConnectClicked()`内で`orientationReverse.isChecked`を1回だけ読む（`putExtra("reverse", ...)`）→ 自動検知はConnect押下前にチェックボックス状態を正しくしておけばよく、タイミング上の特別な配慮は不要
  - `onDestroy()`はあるが`onResume()`/`onPause()`は無い → センサーリスナー登録・解除のため新規追加が必要

## 判定ロジック

`MainActivity`は`requestedOrientation`でlandscapeに固定表示されている。この状態での`Display.getRotation()`（`Surface.ROTATION_0/90/180/270`）は、端末のnatural orientation（多くの端末はportraitだが、一部タブレットはlandscapeの場合がある）に対する現在の実際の回転を反映する。これを使い、natural orientationに依存せず判定できるロジックにする：

1. **natural orientation判定**（実行時に1回導出）: `display.rotation`が0/180なら natural=landscape、90/270なら natural=portrait
2. **物理角度算出**: `TYPE_GRAVITY`の値からデバイス座標系のまま`atan2(-gravity[0], gravity[1])`で0〜360度の角度を算出（Androidの`OrientationEventListener`と同様の標準的手法）
3. **4状態へのマッピング**: 手順1で導出したnatural orientationを使い、角度を「portrait正常/反転」「landscape正常/反転」の4つの基準角（0/90/180/270の並び順がnatural orientationに応じて入れ替わる）に対応付ける
4. `orientationMode`ラジオボタンの現在選択（portrait/landscape）と組み合わせ、「正常」か「反転」かを1つのbool値として確定する

**確信度**: phone側（実機Moto G66j 5G、Pixel 9 Pro、いずれもnatural=portrait）はこのロジックで動作する見込みが高い。landscape-natural端末（一部タブレット）での実機検証はまだ行っていないため中確信度。手動オーバーライドが常に効くため、判定を誤っても機能上のブロッカーにはならない。

## ちらつき防止

- 閾値: 重力成分の絶対値が一定値（目安3.0 m/s²）以上のときのみ状態を確定
- エッジトリガー: 確定した物理状態が直前の確定状態と異なる場合のみ`orientationReverse.setChecked()`を呼ぶ（毎サンプルでは呼ばない）

## 手動上書きとの共存

- ユーザーがチェックボックスを手動タップしたら、その値をそのまま保持する
- 次に物理的な向きの「遷移」が実際に検知されたときだけ、自動検知が再度上書きする
- 同じ物理姿勢が続く間は手動操作が優先される

## センサー・ライフサイクル

- `SensorManager.getDefaultSensor(Sensor.TYPE_GRAVITY)`（`TYPE_ACCELEROMETER`より高周波ノイズが少ない）
- `onResume()`で`registerListener`、`onPause()`で`unregisterListener`（両メソッドを新規追加）

## Codexクロスチェックについて

org方針に基づき`codex exec`でセカンドオピニオンを試みたが、`ERROR: Your workspace is out of credits.`で失敗（レビュー内容ゼロ）。過去のハング事例（`codex-inline`プラグイン、2時間無応答）とは別の失敗モード。前例（2026-07-09の複数台接続修正時の対応）に従い、Codex側の意見なしとして扱い、Claude自身のレビューのみを根拠に本設計とする。自己レビューで元案の「natural orientation=portrait前提、タブレット非対応」という弱点を見直し、`Display.getRotation()`を使った実行時導出でカバーする形に修正した。

## 未検証・リスク

- landscape-natural端末での実機検証なし（手動オーバーライドがフォールバックになるため機能停止にはならない）
- `atan2`による基準角とAndroidの`ActivityInfo.SCREEN_ORIENTATION_*`規約との対応関係は、実機での動作確認で符号・オフセットを調整する必要がある可能性がある
