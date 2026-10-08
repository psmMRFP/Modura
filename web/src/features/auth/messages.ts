import type { Locale } from "../../app/locales";

type Copy = {
  account: string;
  login: string;
  register: string;
  resend: string;
  recover: string;
  verify: string;
  reset: string;
  email: string;
  password: string;
  code: string;
  submit: string;
  accepted: string;
  completed: string;
  disabled: string;
  loading: string;
  failure: string;
  challenge: string;
  signedIn: string;
  logout: string;
  restore: string;
  passwordRule: string;
  sessionExpired: string;
};
export const authMessages: Record<Locale, Copy> = {
  en: {
    account: "Your account",
    login: "Sign in",
    register: "Create account",
    resend: "Resend verification",
    recover: "Forgot password",
    verify: "Verify email",
    reset: "Reset password",
    email: "Email",
    password: "Password",
    code: "One-time code from your email",
    submit: "Continue",
    accepted:
      "If this request is eligible, an email will arrive shortly. Check your inbox and spam folder.",
    completed: "Done. You can now sign in.",
    disabled:
      "Public accounts are not available yet. You can browse places without an account.",
    loading: "Loading account availability…",
    failure:
      "The request could not be completed. Check your details and try again; requests may be limited.",
    challenge:
      "Complete the security check. If it fails to load, refresh this page.",
    signedIn: "Signed in",
    logout: "Sign out",
    restore: "Restore or refresh your session",
    passwordRule:
      "Use 12–1024 characters. Email verification is required before signing in.",
    sessionExpired:
      "Your session is unavailable or expired. Restore it or sign in again.",
  },
  "zh-CN": {
    account: "我的账户",
    login: "登录",
    register: "注册账户",
    resend: "重发验证邮件",
    recover: "忘记密码",
    verify: "验证邮箱",
    reset: "重置密码",
    email: "邮箱",
    password: "密码",
    code: "邮件中的一次性代码",
    submit: "继续",
    accepted: "如果请求符合条件，我们会发送邮件。请检查收件箱及垃圾邮件。",
    completed: "操作完成，现在可以登录。",
    disabled: "公共账户暂未开放，无需账户即可浏览地点。",
    loading: "正在检查账户功能…",
    failure: "请求未完成，请检查输入后重试；请求可能已达到限额。",
    challenge: "请完成安全验证。若无法加载，请刷新页面。",
    signedIn: "已登录",
    logout: "退出登录",
    restore: "恢复或刷新会话",
    passwordRule: "密码使用 12–1024 个字符，登录前须验证邮箱。",
    sessionExpired: "会话不可用或已过期，请恢复会话或重新登录。",
  },
  de: {
    account: "Dein Konto",
    login: "Anmelden",
    register: "Konto erstellen",
    resend: "Bestätigung erneut senden",
    recover: "Passwort vergessen",
    verify: "E-Mail bestätigen",
    reset: "Passwort zurücksetzen",
    email: "E-Mail",
    password: "Passwort",
    code: "Einmalcode aus der E-Mail",
    submit: "Weiter",
    accepted:
      "Wenn die Anfrage berechtigt ist, erhältst du eine E-Mail. Prüfe auch den Spamordner.",
    completed: "Erledigt. Du kannst dich jetzt anmelden.",
    disabled:
      "Öffentliche Konten sind noch nicht verfügbar. Orte kannst du ohne Konto ansehen.",
    loading: "Kontoverfügbarkeit wird geladen…",
    failure:
      "Die Anfrage konnte nicht abgeschlossen werden. Prüfe deine Angaben und versuche es erneut; Anfragen können begrenzt sein.",
    challenge:
      "Schließe die Sicherheitsprüfung ab. Wird sie nicht geladen, aktualisiere die Seite.",
    signedIn: "Angemeldet",
    logout: "Abmelden",
    restore: "Sitzung wiederherstellen oder erneuern",
    passwordRule:
      "Verwende 12–1024 Zeichen. Vor der Anmeldung muss die E-Mail bestätigt werden.",
    sessionExpired:
      "Die Sitzung ist nicht verfügbar oder abgelaufen. Stelle sie wieder her oder melde dich erneut an.",
  },
  fr: {
    account: "Votre compte",
    login: "Se connecter",
    register: "Créer un compte",
    resend: "Renvoyer la vérification",
    recover: "Mot de passe oublié",
    verify: "Vérifier l’adresse e-mail",
    reset: "Réinitialiser le mot de passe",
    email: "E-mail",
    password: "Mot de passe",
    code: "Code à usage unique reçu par e-mail",
    submit: "Continuer",
    accepted:
      "Si la demande est éligible, vous recevrez un e-mail. Vérifiez aussi les courriers indésirables.",
    completed: "Terminé. Vous pouvez maintenant vous connecter.",
    disabled:
      "Les comptes publics ne sont pas encore disponibles. Vous pouvez consulter les lieux sans compte.",
    loading: "Vérification de la disponibilité des comptes…",
    failure:
      "La demande n’a pas pu aboutir. Vérifiez vos informations et réessayez ; les demandes peuvent être limitées.",
    challenge:
      "Effectuez la vérification de sécurité. Si elle ne se charge pas, actualisez la page.",
    signedIn: "Connecté",
    logout: "Se déconnecter",
    restore: "Restaurer ou renouveler la session",
    passwordRule:
      "Utilisez 12–1024 caractères. L’adresse e-mail doit être vérifiée avant la connexion.",
    sessionExpired:
      "Votre session est indisponible ou expirée. Restaurez-la ou reconnectez-vous.",
  },
  es: {
    account: "Tu cuenta",
    login: "Iniciar sesión",
    register: "Crear cuenta",
    resend: "Reenviar verificación",
    recover: "Olvidé mi contraseña",
    verify: "Verificar correo",
    reset: "Restablecer contraseña",
    email: "Correo electrónico",
    password: "Contraseña",
    code: "Código de un solo uso del correo",
    submit: "Continuar",
    accepted:
      "Si la solicitud cumple los requisitos, recibirás un correo. Revisa también la carpeta de spam.",
    completed: "Listo. Ya puedes iniciar sesión.",
    disabled:
      "Las cuentas públicas aún no están disponibles. Puedes consultar lugares sin cuenta.",
    loading: "Comprobando la disponibilidad de cuentas…",
    failure:
      "No se pudo completar la solicitud. Comprueba los datos e inténtalo de nuevo; las solicitudes pueden estar limitadas.",
    challenge:
      "Completa la verificación de seguridad. Si no se carga, actualiza la página.",
    signedIn: "Sesión iniciada",
    logout: "Cerrar sesión",
    restore: "Restaurar o renovar la sesión",
    passwordRule:
      "Usa 12–1024 caracteres. Debes verificar el correo antes de iniciar sesión.",
    sessionExpired:
      "La sesión no está disponible o ha caducado. Restáurala o inicia sesión de nuevo.",
  },
};
