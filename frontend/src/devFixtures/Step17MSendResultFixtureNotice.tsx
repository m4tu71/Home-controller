import styles from "../pages/web-controller/WebController.module.css";

export function Step17MSendResultFixtureNotice() {
  return (
    <div className={styles.warningText} role="alert">
      Step 17M送信結果fixture：controller APIはブラウザ内のmockです。送信POSTも実Backend・実機へ届きません。送信結果の表示だけを確認してください。
    </div>
  );
}
