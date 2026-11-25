# UniPatcher (万能ロムパッチャー)

UniPatcher は、いろいろな形式のロムパッチを当てたり、パッチを作ったりできる単体のソフトです。

UniPatcher is a standalone tool that applies ROM patches in many formats and can also create patches.

インストールは不要で、exe を起動するだけで使えます。

No installation is needed; just run the exe.

## できること / What it does

「パッチを当てる」ページでは、パッチファイルを元のロムに当てて、パッチ適用済みのロムを作ります。

On the "Apply patch" page, you apply a patch file to an original ROM and get a patched ROM.

元のロムファイルは変更されず、結果は新しいファイルとしてダウンロードされます。

The original ROM file is never changed, and the result is downloaded as a new file.

「パッチを作成」ページでは、元のロムと改造済みロムを比べて、差分のパッチファイルを作ります。

On the "Create patch" page, you compare an original ROM with a modified ROM and create a patch file of the differences.

作ったパッチは、元のロムを持っている人に配布できます。

You can give the patch to anyone who owns the original ROM.

ロムそのものではなく差分だけを配るので、ファイルサイズを小さくできます。

Only the differences are shared, so the patch file stays small.

すべてのファイル欄は、クリックしてファイルを開く操作と、ドラッグ&ドロップの両方に対応しています。

Every file field supports both clicking to open a file and drag & drop.

## 対応形式 / Supported formats

当てることができる形式は IPS、IPS32、UPS、BPS、APS (GBA)、PPF (1.0/2.0/3.0)、xdelta (VCDIFF) です。

You can apply IPS, IPS32, UPS, BPS, APS (GBA), PPF (1.0/2.0/3.0) and xdelta (VCDIFF) patches.

作成できる形式は IPS、UPS、BPS、xdelta (VCDIFF) です。

You can create IPS, UPS, BPS and xdelta (VCDIFF) patches.

形式はファイルの中身から自動で判定するので、拡張子が違っていてもかまいません。

The format is detected from the file contents, so a wrong file extension is not a problem.

xdelta の二次圧縮 (FGK など) が使われているパッチは未対応で、その場合はエラーが表示されます。

Patches that use xdelta secondary compression (such as FGK) are not supported, and an error is shown for them.

## CRC について / About CRC

CRC32 は、ファイルの内容を表す短いチェック値です。

CRC32 is a short checksum that represents the contents of a file.

UPS と BPS のパッチには、元のロムの CRC32 が記録されています。

UPS and BPS patches store the CRC32 of the original ROM they expect.

パッチを選ぶと、その CRC がパッチ欄の横に表示されます。

When you choose a patch, that CRC is shown next to the patch field.

ロムを選ぶと、そのロムの CRC がロム欄の横に表示されます。

When you choose a ROM, the ROM's CRC is shown next to the ROM field.

両方の CRC が一致すれば、そのパッチが想定しているロムだと分かります。

If both CRCs match, you know the patch was made for that ROM.

IPS など CRC が記録されていない形式では、照合はできません。

Formats that store no CRC, such as IPS, cannot be checked this way.

設定ページでは、CRC が一致しないときの動きを「警告して選べる」「中止する」「チェックしない」から選べます。

On the Settings page, you can choose what happens when the CRC does not match: warn and let you choose, stop, or do not check.

## 設定 / Settings

表示言語は12言語から選べ、初期値はシステム言語です。

The display language can be chosen from 12 languages, and the default is the system language.

MD5 と SHA-1 の表示も設定ページで切り替えられます。

Showing MD5 and SHA-1 can also be toggled on the Settings page.

設定はこの端末に自動で保存され、次回起動時にも使われます。

Settings are saved on this device automatically and are used again on the next start.
