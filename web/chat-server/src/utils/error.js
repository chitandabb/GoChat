// 统一的错误信息提取：axios 错误响应体（业务 message）> Error.message（如网络错误）> 兜底文案。
// 避免到处把 Error 对象直接塞进 ElMessage.error() 显示成 "[object Object]"。
export function errorMsg(error, fallback = "操作失败，请稍后重试") {
  if (!error) {
    return fallback;
  }
  if (error.response && error.response.data) {
    const data = error.response.data;
    if (data.message) {
      return data.message;
    }
    if (typeof data === "string" && data) {
      return data;
    }
  }
  if (error.message) {
    return error.message;
  }
  if (typeof error === "string" && error) {
    return error;
  }
  return fallback;
}