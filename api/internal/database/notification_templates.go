package database

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

var notificationTemplateLocales = []string{"zh-CN", "zh-TW", "en", "vi", "ru", "ja", "ko", "th"}

var notificationAdminChannels = []string{"admin", "email", "telegram", "wecom"}

var notificationUserChannels = []string{"in_app", "email"}

var notificationAdminEventCodes = []string{"user.registered", "user.login.succeeded", "user.login.failed", "admin.login.failed", "order.created", "order.paid", "order.processing", "order.delivered", "order.failed", "order.refunded", "recharge.created", "recharge.succeeded", "recharge.failed", "openapi.credential.created", "openapi.call.succeeded", "openapi.call.failed", "openapi.order.created", "inventory.low_stock", "inventory.out_of_stock", "inventory.restocked", "supplier.sync.succeeded", "supplier.sync.failed", "procurement.created", "procurement.succeeded", "procurement.failed", "risk.blocked", "security.high_risk"}

var notificationUserEventCodes = []string{"user.registered", "user.login.succeeded", "user.login.failed", "order.created", "order.paid", "order.processing", "order.delivered", "order.failed", "order.refunded", "recharge.created", "recharge.succeeded", "recharge.failed", "openapi.order.created"}

// The localized event names deliberately follow notificationAdminEventCodes.
// Keeping the operational code in the body as well makes alerts searchable
// while the subject remains useful to a human recipient.
var notificationLocalizedEventNames = map[string][]string{
	"zh-CN": {"用户注册完成", "用户登录成功", "用户登录失败", "管理员登录失败", "订单已创建", "订单支付成功", "订单进入交付", "订单交付完成", "订单交付失败", "订单退款完成", "充值单已创建", "充值已到账", "充值失败", "OpenAPI 凭证已创建", "OpenAPI 调用成功", "OpenAPI 调用失败", "OpenAPI 订单已创建", "库存低于预警线", "库存已售罄", "库存补货完成", "上游同步成功", "上游同步失败", "采购单已创建", "上游采购成功", "上游采购失败", "风控已拒绝交易", "检测到高风险安全事件"},
	"zh-TW": {"使用者註冊完成", "使用者登入成功", "使用者登入失敗", "管理員登入失敗", "訂單已建立", "訂單付款成功", "訂單進入交付", "訂單交付完成", "訂單交付失敗", "訂單退款完成", "充值單已建立", "充值已入帳", "充值失敗", "OpenAPI 憑證已建立", "OpenAPI 呼叫成功", "OpenAPI 呼叫失敗", "OpenAPI 訂單已建立", "庫存低於警戒線", "庫存已售罄", "庫存補貨完成", "上游同步成功", "上游同步失敗", "採購單已建立", "上游採購成功", "上游採購失敗", "風控已拒絕交易", "偵測到高風險安全事件"},
	"en":    {"User registration completed", "User sign-in succeeded", "User sign-in failed", "Administrator sign-in failed", "Order created", "Order payment succeeded", "Order entered fulfillment", "Order delivery completed", "Order delivery failed", "Order refund completed", "Recharge created", "Recharge credited", "Recharge failed", "OpenAPI credential created", "OpenAPI call succeeded", "OpenAPI call failed", "OpenAPI order created", "Stock below warning threshold", "Product out of stock", "Inventory replenished", "Supplier synchronization succeeded", "Supplier synchronization failed", "Procurement created", "Supplier procurement succeeded", "Supplier procurement failed", "Transaction blocked by risk control", "High-risk security event detected"},
	"vi":    {"Đăng ký người dùng hoàn tất", "Đăng nhập người dùng thành công", "Đăng nhập người dùng thất bại", "Đăng nhập quản trị viên thất bại", "Đơn hàng đã được tạo", "Thanh toán đơn hàng thành công", "Đơn hàng bắt đầu giao", "Giao hàng hoàn tất", "Giao hàng thất bại", "Hoàn tiền hoàn tất", "Yêu cầu nạp tiền đã tạo", "Tiền nạp đã vào tài khoản", "Nạp tiền thất bại", "Đã tạo thông tin xác thực OpenAPI", "Lệnh gọi OpenAPI thành công", "Lệnh gọi OpenAPI thất bại", "Đơn hàng OpenAPI đã tạo", "Tồn kho dưới ngưỡng cảnh báo", "Sản phẩm đã hết hàng", "Đã bổ sung tồn kho", "Đồng bộ nhà cung cấp thành công", "Đồng bộ nhà cung cấp thất bại", "Đơn mua hàng đã tạo", "Mua hàng từ nhà cung cấp thành công", "Mua hàng từ nhà cung cấp thất bại", "Giao dịch bị kiểm soát rủi ro chặn", "Phát hiện sự kiện bảo mật rủi ro cao"},
	"ru":    {"Регистрация пользователя завершена", "Вход пользователя выполнен", "Ошибка входа пользователя", "Ошибка входа администратора", "Заказ создан", "Заказ успешно оплачен", "Заказ передан на исполнение", "Доставка заказа завершена", "Ошибка доставки заказа", "Возврат по заказу завершён", "Заявка на пополнение создана", "Пополнение зачислено", "Ошибка пополнения", "Учётные данные OpenAPI созданы", "Вызов OpenAPI выполнен", "Ошибка вызова OpenAPI", "Заказ OpenAPI создан", "Остаток ниже порога", "Товар закончился", "Запас пополнен", "Синхронизация поставщика выполнена", "Ошибка синхронизации поставщика", "Закупка создана", "Закупка у поставщика выполнена", "Ошибка закупки у поставщика", "Транзакция заблокирована контролем риска", "Обнаружено событие высокого риска"},
	"ja":    {"ユーザー登録が完了", "ユーザーログイン成功", "ユーザーログイン失敗", "管理者ログイン失敗", "注文を作成", "注文の支払い成功", "注文が交付処理へ移行", "注文の交付完了", "注文の交付失敗", "注文の返金完了", "チャージを作成", "チャージ入金完了", "チャージ失敗", "OpenAPI 認証情報を作成", "OpenAPI 呼び出し成功", "OpenAPI 呼び出し失敗", "OpenAPI 注文を作成", "在庫が警告値未満", "在庫切れ", "在庫補充完了", "仕入先同期成功", "仕入先同期失敗", "仕入れ注文を作成", "仕入先からの購入成功", "仕入先からの購入失敗", "リスク管理が取引を拒否", "高リスクセキュリティイベントを検出"},
	"ko":    {"사용자 가입 완료", "사용자 로그인 성공", "사용자 로그인 실패", "관리자 로그인 실패", "주문 생성", "주문 결제 성공", "주문 배송 처리 시작", "주문 배송 완료", "주문 배송 실패", "주문 환불 완료", "충전 주문 생성", "충전 금액 반영", "충전 실패", "OpenAPI 자격 증명 생성", "OpenAPI 호출 성공", "OpenAPI 호출 실패", "OpenAPI 주문 생성", "재고가 경고 기준 미만", "재고 소진", "재고 보충 완료", "공급업체 동기화 성공", "공급업체 동기화 실패", "조달 주문 생성", "공급업체 구매 성공", "공급업체 구매 실패", "위험 관리에서 거래 차단", "고위험 보안 이벤트 감지"},
	"th":    {"ลงทะเบียนผู้ใช้สำเร็จ", "ผู้ใช้เข้าสู่ระบบสำเร็จ", "ผู้ใช้เข้าสู่ระบบไม่สำเร็จ", "ผู้ดูแลเข้าสู่ระบบไม่สำเร็จ", "สร้างคำสั่งซื้อแล้ว", "ชำระคำสั่งซื้อสำเร็จ", "คำสั่งซื้อเข้าสู่ขั้นตอนส่งมอบ", "ส่งมอบคำสั่งซื้อสำเร็จ", "ส่งมอบคำสั่งซื้อไม่สำเร็จ", "คืนเงินคำสั่งซื้อสำเร็จ", "สร้างรายการเติมเงินแล้ว", "ยอดเติมเงินเข้าบัญชีแล้ว", "เติมเงินไม่สำเร็จ", "สร้างข้อมูลรับรอง OpenAPI แล้ว", "เรียก OpenAPI สำเร็จ", "เรียก OpenAPI ไม่สำเร็จ", "สร้างคำสั่งซื้อ OpenAPI แล้ว", "สต็อกต่ำกว่าระดับเตือน", "สินค้าหมดสต็อก", "เติมสต็อกสำเร็จ", "ซิงค์ซัพพลายเออร์สำเร็จ", "ซิงค์ซัพพลายเออร์ไม่สำเร็จ", "สร้างรายการจัดซื้อแล้ว", "จัดซื้อจากซัพพลายเออร์สำเร็จ", "จัดซื้อจากซัพพลายเออร์ไม่สำเร็จ", "ระบบความเสี่ยงปฏิเสธธุรกรรม", "ตรวจพบเหตุการณ์ความปลอดภัยความเสี่ยงสูง"},
}

type notificationLocaleCopy struct {
	AdminName string
	UserName  string
	Subject   string
	AdminBody string
	UserBody  string
}

var notificationLocaleCopies = map[string]notificationLocaleCopy{
	"zh-CN": {
		AdminName: "管理员运营通知", UserName: "用户业务通知", Subject: "[LinLinQi] %s · {{status}}",
		AdminBody: "LinLinQi 运营中心检测到需要关注的业务事件。\n\n事件类型：%s\n事件代码：{{event}}\n业务摘要：{{summary}}\n当前状态：{{status}}\n订单编号：{{order_no}}\n商品名称：{{product_name}}\n金额与币种：{{amount}} {{currency}}\n当前库存：{{stock}}\n来源渠道：{{channel}}\n来源 IP：{{ip}}\n业务实体：{{entity_id}}\n发生时间：{{occurred_at}}\n\n处理建议：请在管理后台核对关联订单、支付、库存、供货或安全记录；涉及失败、拒绝、异常登录及库存告警时，请保留审计证据并及时处理。此消息仅发送给后台管理员，禁止转发给前台用户。",
		UserBody:  "您好，您的 LinLinQi 账户发生了一项业务状态更新。\n\n通知类型：%s\n业务摘要：{{summary}}\n当前状态：{{status}}\n订单或充值编号：{{order_no}}\n商品名称：{{product_name}}\n金额与币种：{{amount}} {{currency}}\n处理渠道：{{channel}}\n更新时间：{{occurred_at}}\n\n请登录 LinLinQi 账户中心查看完整记录。若该操作并非由您发起，请立即修改密码并联系平台客服。本通知仅包含您的账户信息，不包含后台运营或其他用户数据。",
	},
	"zh-TW": {
		AdminName: "管理員營運通知", UserName: "使用者業務通知", Subject: "[LinLinQi] %s · {{status}}",
		AdminBody: "LinLinQi 營運中心偵測到需要關注的業務事件。\n\n事件類型：%s\n事件代碼：{{event}}\n業務摘要：{{summary}}\n目前狀態：{{status}}\n訂單編號：{{order_no}}\n商品名稱：{{product_name}}\n金額與幣別：{{amount}} {{currency}}\n目前庫存：{{stock}}\n來源管道：{{channel}}\n來源 IP：{{ip}}\n業務實體：{{entity_id}}\n發生時間：{{occurred_at}}\n\n處理建議：請在管理後台核對相關訂單、付款、庫存、供應或安全記錄；如涉及失敗、拒絕、異常登入或庫存警示，請保存稽核證據並立即處理。本訊息僅供後台管理員，禁止轉發給前台使用者。",
		UserBody:  "您好，您的 LinLinQi 帳戶有一項業務狀態更新。\n\n通知類型：%s\n業務摘要：{{summary}}\n目前狀態：{{status}}\n訂單或充值編號：{{order_no}}\n商品名稱：{{product_name}}\n金額與幣別：{{amount}} {{currency}}\n處理管道：{{channel}}\n更新時間：{{occurred_at}}\n\n請登入 LinLinQi 帳戶中心查看完整記錄。若此操作並非由您發起，請立即變更密碼並聯絡客服。本通知只包含您的帳戶資訊，不包含後台營運或其他使用者資料。",
	},
	"en": {
		AdminName: "Administrator operations alert", UserName: "Customer account notification", Subject: "[LinLinQi] %s · {{status}}",
		AdminBody: "LinLinQi Operations detected a business event that requires attention.\n\nEvent type: %s\nEvent code: {{event}}\nSummary: {{summary}}\nCurrent status: {{status}}\nOrder number: {{order_no}}\nProduct: {{product_name}}\nAmount and currency: {{amount}} {{currency}}\nCurrent stock: {{stock}}\nSource channel: {{channel}}\nSource IP: {{ip}}\nEntity ID: {{entity_id}}\nOccurred at: {{occurred_at}}\n\nRecommended action: review the related order, payment, inventory, supplier, or security record in the administration console. Preserve audit evidence and act promptly for failures, denials, unusual logins, and stock alerts. This message is for administrators only and must never be forwarded to storefront users.",
		UserBody:  "Hello, a business status associated with your LinLinQi account has changed.\n\nNotification type: %s\nSummary: {{summary}}\nCurrent status: {{status}}\nOrder or recharge number: {{order_no}}\nProduct: {{product_name}}\nAmount and currency: {{amount}} {{currency}}\nProcessing channel: {{channel}}\nUpdated at: {{occurred_at}}\n\nSign in to the LinLinQi account center to review the complete record. If you did not initiate this activity, change your password immediately and contact support. This notification contains only your account information and never exposes administrator or other customer data.",
	},
	"vi": {
		AdminName: "Thông báo vận hành quản trị", UserName: "Thông báo tài khoản người dùng", Subject: "[LinLinQi] %s · {{status}}",
		AdminBody: "Trung tâm vận hành LinLinQi phát hiện một sự kiện kinh doanh cần được chú ý.\n\nLoại sự kiện: %s\nMã sự kiện: {{event}}\nTóm tắt: {{summary}}\nTrạng thái hiện tại: {{status}}\nMã đơn hàng: {{order_no}}\nSản phẩm: {{product_name}}\nSố tiền và tiền tệ: {{amount}} {{currency}}\nTồn kho hiện tại: {{stock}}\nKênh nguồn: {{channel}}\nIP nguồn: {{ip}}\nMã đối tượng: {{entity_id}}\nThời gian xảy ra: {{occurred_at}}\n\nKhuyến nghị: kiểm tra đơn hàng, thanh toán, tồn kho, nhà cung cấp hoặc bản ghi bảo mật liên quan trong trang quản trị. Hãy lưu bằng chứng kiểm toán và xử lý ngay các lỗi, từ chối, đăng nhập bất thường hoặc cảnh báo tồn kho. Thông báo này chỉ dành cho quản trị viên và không được chuyển cho người dùng.",
		UserBody:  "Xin chào, trạng thái nghiệp vụ liên quan đến tài khoản LinLinQi của bạn vừa thay đổi.\n\nLoại thông báo: %s\nTóm tắt: {{summary}}\nTrạng thái hiện tại: {{status}}\nMã đơn hoặc nạp tiền: {{order_no}}\nSản phẩm: {{product_name}}\nSố tiền và tiền tệ: {{amount}} {{currency}}\nKênh xử lý: {{channel}}\nThời gian cập nhật: {{occurred_at}}\n\nVui lòng đăng nhập trung tâm tài khoản LinLinQi để xem đầy đủ. Nếu bạn không thực hiện hoạt động này, hãy đổi mật khẩu ngay và liên hệ hỗ trợ. Thông báo chỉ chứa dữ liệu tài khoản của bạn, không chứa dữ liệu quản trị hoặc người dùng khác.",
	},
	"ru": {
		AdminName: "Операционное уведомление администратора", UserName: "Уведомление пользователя", Subject: "[LinLinQi] %s · {{status}}",
		AdminBody: "Операционный центр LinLinQi обнаружил событие, требующее внимания.\n\nТип события: %s\nКод события: {{event}}\nСводка: {{summary}}\nТекущий статус: {{status}}\nНомер заказа: {{order_no}}\nТовар: {{product_name}}\nСумма и валюта: {{amount}} {{currency}}\nТекущий остаток: {{stock}}\nКанал: {{channel}}\nIP-адрес: {{ip}}\nИдентификатор объекта: {{entity_id}}\nВремя события: {{occurred_at}}\n\nРекомендация: проверьте связанные заказы, платежи, остатки, поставщиков и события безопасности в панели управления. Сохраняйте аудиторские доказательства и без задержки обрабатывайте ошибки, отказы, необычные входы и предупреждения об остатках. Сообщение предназначено только администраторам и не должно передаваться пользователям.",
		UserBody:  "Здравствуйте! Статус операции, связанной с вашей учетной записью LinLinQi, изменился.\n\nТип уведомления: %s\nСводка: {{summary}}\nТекущий статус: {{status}}\nНомер заказа или пополнения: {{order_no}}\nТовар: {{product_name}}\nСумма и валюта: {{amount}} {{currency}}\nКанал обработки: {{channel}}\nВремя обновления: {{occurred_at}}\n\nВойдите в центр учетной записи LinLinQi для просмотра полной записи. Если действие выполнено не вами, немедленно смените пароль и обратитесь в поддержку. Уведомление содержит только данные вашей учетной записи и не раскрывает данные администраторов или других пользователей.",
	},
	"ja": {
		AdminName: "管理者向け運用通知", UserName: "ユーザーアカウント通知", Subject: "[LinLinQi] %s · {{status}}",
		AdminBody: "LinLinQi 運用センターが確認を必要とする業務イベントを検出しました。\n\nイベント種別：%s\nイベントコード：{{event}}\n概要：{{summary}}\n現在の状態：{{status}}\n注文番号：{{order_no}}\n商品：{{product_name}}\n金額と通貨：{{amount}} {{currency}}\n現在庫：{{stock}}\n送信元チャネル：{{channel}}\n送信元 IP：{{ip}}\nエンティティ ID：{{entity_id}}\n発生日時：{{occurred_at}}\n\n推奨対応：管理画面で関連する注文、決済、在庫、仕入先、セキュリティ記録を確認してください。失敗、拒否、不審なログイン、在庫警告については監査証跡を保存し、速やかに対応してください。この通知は管理者専用であり、ユーザーへ転送してはいけません。",
		UserBody:  "お客様の LinLinQi アカウントに関連する業務状態が更新されました。\n\n通知種別：%s\n概要：{{summary}}\n現在の状態：{{status}}\n注文またはチャージ番号：{{order_no}}\n商品：{{product_name}}\n金額と通貨：{{amount}} {{currency}}\n処理チャネル：{{channel}}\n更新日時：{{occurred_at}}\n\nLinLinQi アカウントセンターにログインして詳細をご確認ください。心当たりがない場合は、直ちにパスワードを変更してサポートへご連絡ください。この通知にはお客様自身の情報のみが含まれ、管理者や他の利用者の情報は含まれません。",
	},
	"ko": {
		AdminName: "관리자 운영 알림", UserName: "사용자 계정 알림", Subject: "[LinLinQi] %s · {{status}}",
		AdminBody: "LinLinQi 운영 센터에서 확인이 필요한 비즈니스 이벤트를 감지했습니다.\n\n이벤트 유형: %s\n이벤트 코드: {{event}}\n요약: {{summary}}\n현재 상태: {{status}}\n주문 번호: {{order_no}}\n상품: {{product_name}}\n금액 및 통화: {{amount}} {{currency}}\n현재 재고: {{stock}}\n발생 채널: {{channel}}\n발생 IP: {{ip}}\n엔터티 ID: {{entity_id}}\n발생 시간: {{occurred_at}}\n\n권장 조치: 관리자 화면에서 관련 주문, 결제, 재고, 공급업체 또는 보안 기록을 확인하십시오. 실패, 거부, 비정상 로그인 및 재고 경고는 감사 증거를 보존하고 즉시 처리해야 합니다. 이 메시지는 관리자 전용이며 사용자에게 전달해서는 안 됩니다.",
		UserBody:  "LinLinQi 계정과 관련된 업무 상태가 변경되었습니다.\n\n알림 유형: %s\n요약: {{summary}}\n현재 상태: {{status}}\n주문 또는 충전 번호: {{order_no}}\n상품: {{product_name}}\n금액 및 통화: {{amount}} {{currency}}\n처리 채널: {{channel}}\n업데이트 시간: {{occurred_at}}\n\n전체 기록은 LinLinQi 계정 센터에 로그인하여 확인하십시오. 본인이 요청하지 않은 활동이라면 즉시 비밀번호를 변경하고 고객지원에 문의하십시오. 이 알림에는 본인 계정 정보만 포함되며 관리자 또는 다른 사용자의 데이터는 포함되지 않습니다.",
	},
	"th": {
		AdminName: "การแจ้งเตือนการดำเนินงานผู้ดูแล", UserName: "การแจ้งเตือนบัญชีผู้ใช้", Subject: "[LinLinQi] %s · {{status}}",
		AdminBody: "ศูนย์ปฏิบัติการ LinLinQi ตรวจพบเหตุการณ์ทางธุรกิจที่ต้องตรวจสอบ\n\nประเภทเหตุการณ์: %s\nรหัสเหตุการณ์: {{event}}\nสรุป: {{summary}}\nสถานะปัจจุบัน: {{status}}\nหมายเลขคำสั่งซื้อ: {{order_no}}\nสินค้า: {{product_name}}\nจำนวนเงินและสกุลเงิน: {{amount}} {{currency}}\nสินค้าคงเหลือ: {{stock}}\nช่องทางต้นทาง: {{channel}}\nIP ต้นทาง: {{ip}}\nรหัสเอนทิตี: {{entity_id}}\nเวลาที่เกิด: {{occurred_at}}\n\nคำแนะนำ: ตรวจสอบคำสั่งซื้อ การชำระเงิน สินค้าคงคลัง ซัพพลายเออร์ หรือบันทึกความปลอดภัยที่เกี่ยวข้องในหน้าผู้ดูแล เก็บหลักฐานการตรวจสอบและดำเนินการทันทีสำหรับความล้มเหลว การปฏิเสธ การเข้าสู่ระบบผิดปกติ และการเตือนสต็อก ข้อความนี้มีไว้สำหรับผู้ดูแลเท่านั้นและห้ามส่งต่อให้ผู้ใช้หน้าร้าน",
		UserBody:  "สถานะทางธุรกิจที่เกี่ยวข้องกับบัญชี LinLinQi ของคุณมีการเปลี่ยนแปลง\n\nประเภทการแจ้งเตือน: %s\nสรุป: {{summary}}\nสถานะปัจจุบัน: {{status}}\nหมายเลขคำสั่งซื้อหรือเติมเงิน: {{order_no}}\nสินค้า: {{product_name}}\nจำนวนเงินและสกุลเงิน: {{amount}} {{currency}}\nช่องทางดำเนินการ: {{channel}}\nเวลาอัปเดต: {{occurred_at}}\n\nโปรดเข้าสู่ศูนย์บัญชี LinLinQi เพื่อดูรายละเอียดทั้งหมด หากคุณไม่ได้เริ่มกิจกรรมนี้ ให้เปลี่ยนรหัสผ่านทันทีและติดต่อฝ่ายสนับสนุน การแจ้งเตือนนี้มีเฉพาะข้อมูลบัญชีของคุณและไม่เปิดเผยข้อมูลผู้ดูแลหรือผู้ใช้อื่น",
	},
}

func localeCodeSuffix(locale string) string {
	return strings.ToLower(strings.ReplaceAll(locale, "-", "_"))
}

func localizedNotificationEventName(locale, eventCode string) string {
	names := notificationLocalizedEventNames[locale]
	for index, code := range notificationAdminEventCodes {
		if code == eventCode && index < len(names) {
			return names[index]
		}
	}
	return eventCode
}

func notificationTemplateCode(audience, eventCode, channel, locale string) string {
	event := strings.ReplaceAll(eventCode, ".", "_")
	if audience == "admin" && channel == "admin" {
		return "admin." + event + ".inbox." + localeCodeSuffix(locale)
	}
	if audience == "user" && channel == "in_app" {
		return "user." + event + ".inapp." + localeCodeSuffix(locale)
	}
	return audience + "." + event + "." + channel + "." + localeCodeSuffix(locale)
}

func seedDetailedNotificationTemplates(db *gorm.DB) error {
	adminVariables := `["event","occurred_at","summary","entity_id","status","amount","currency","ip","stock","product_name","order_no","channel"]`
	userVariables := `["occurred_at","summary","status","amount","currency","product_name","order_no","channel"]`
	for _, locale := range notificationTemplateLocales {
		copy := notificationLocaleCopies[locale]
		for _, eventCode := range notificationAdminEventCodes {
			eventName := localizedNotificationEventName(locale, eventCode)
			for _, channel := range notificationAdminChannels {
				template := model.NotificationTemplate{Code: notificationTemplateCode("admin", eventCode, channel, locale), Name: copy.AdminName + " · " + eventName + " · " + channel, Audience: "admin", Channel: channel, Locale: locale, Subject: fmt.Sprintf(copy.Subject, eventName), Body: fmt.Sprintf(copy.AdminBody, eventName), Variables: adminVariables, Enabled: true, Version: 2}
				if err := upsertNotificationTemplate(db, &template); err != nil {
					return err
				}
				if channel == "admin" {
					if err := upsertDefaultNotificationSubscription(db, template, eventCode, "all", locale == "zh-CN"); err != nil {
						return err
					}
				}
			}
		}
		for _, eventCode := range notificationUserEventCodes {
			eventName := localizedNotificationEventName(locale, eventCode)
			for _, channel := range notificationUserChannels {
				template := model.NotificationTemplate{Code: notificationTemplateCode("user", eventCode, channel, locale), Name: copy.UserName + " · " + eventName + " · " + channel, Audience: "user", Channel: channel, Locale: locale, Subject: fmt.Sprintf(copy.Subject, eventName), Body: fmt.Sprintf(copy.UserBody, eventName), Variables: userVariables, Enabled: true, Version: 2}
				if err := upsertNotificationTemplate(db, &template); err != nil {
					return err
				}
				if channel == "in_app" {
					if err := upsertDefaultNotificationSubscription(db, template, eventCode, "event_user", true); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func upsertNotificationTemplate(db *gorm.DB, template *model.NotificationTemplate) error {
	updates := map[string]any{"name": template.Name, "audience": template.Audience, "channel": template.Channel, "locale": template.Locale, "subject": template.Subject, "body": template.Body, "variables": template.Variables, "enabled": true, "version": template.Version}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "code"}}, DoUpdates: clause.Assignments(updates)}).Create(template).Error; err != nil {
		return err
	}
	// OnConflict updates the stored row but GORM leaves the newly generated ID
	// on the input struct. Querying into that struct would silently add the
	// stale primary key to WHERE and report record-not-found.
	var stored model.NotificationTemplate
	if err := db.Where("code = ?", template.Code).First(&stored).Error; err != nil {
		return err
	}
	*template = stored
	return nil
}

func upsertDefaultNotificationSubscription(db *gorm.DB, template model.NotificationTemplate, eventCode, recipient string, enabled bool) error {
	if template.ID == uuid.Nil {
		if err := db.Where("code = ?", template.Code).First(&template).Error; err != nil {
			return err
		}
	}
	var subscription model.NotificationSubscription
	query := db.Where("audience = ? AND event_code = ? AND channel = ? AND recipient = ? AND locale = ?", template.Audience, eventCode, template.Channel, recipient, template.Locale)
	if err := query.First(&subscription).Error; err == nil {
		return db.Model(&subscription).Updates(map[string]any{"template_id": template.ID, "enabled": enabled}).Error
	} else if err != gorm.ErrRecordNotFound {
		return err
	}
	subscription = model.NotificationSubscription{Audience: template.Audience, EventCode: eventCode, Channel: template.Channel, Recipient: recipient, TemplateID: template.ID, Locale: template.Locale, Enabled: enabled}
	return db.Create(&subscription).Error
}
