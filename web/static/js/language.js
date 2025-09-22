/**
 * NPS Web管理界面多语言支持模块
 * 提供国际化功能，包括语言切换、文本翻译、Cookie管理等
 * 依赖jQuery库
 */
(function ($) {

	/**
	 * 将XML数据转换为JSON对象
	 * @param {jQuery} Xml - jQuery包装的XML元素
	 * @returns {Object} 转换后的JSON对象
	 */
	function xml2json(Xml) {
		var tempvalue, tempJson = {};
		$(Xml).each(function() {
			// 获取标签名，优先使用id属性，否则使用标签名
			var tagName = ($(this).attr('id') || this.tagName);
			// 如果没有子元素，直接取文本内容；否则递归处理子元素
			tempvalue = (this.childElementCount == 0) ? this.textContent : xml2json($(this).children());
			
			// 根据现有属性类型处理数据结构
			switch ($.type(tempJson[tagName])) {
				case 'undefined':
					// 首次出现，直接赋值
					tempJson[tagName] = tempvalue;
					break;
				case 'object':
					// 已存在对象，转换为数组
					tempJson[tagName] = Array(tempJson[tagName]);
				case 'array':
					// 数组类型，添加新元素
					tempJson[tagName].push(tempvalue);
			}
		});
		return tempJson;
	}

	/**
	 * 设置Cookie
	 * @param {string} c_name - Cookie名称
	 * @param {string} value - Cookie值
	 * @param {number} expiredays - 过期天数，null表示会话Cookie
	 */
	function setCookie (c_name, value, expiredays) {
		var exdate = new Date();
		exdate.setDate(exdate.getDate() + expiredays);
		// 设置Cookie，包含路径信息
		document.cookie = c_name + '=' + escape(value) + ((expiredays == null) ? '' : ';expires=' + exdate.toGMTString())+ '; path='+window.nps.web_base_url+'/;';
	}

	/**
	 * 获取指定名称的Cookie值
	 * @param {string} c_name - Cookie名称
	 * @returns {string|null} Cookie值，不存在时返回null
	 */
	function getCookie (c_name) {
		if (document.cookie.length > 0) {
			// 查找Cookie名称的位置
			c_start = document.cookie.indexOf(c_name + '=');
			if (c_start != -1) {
				// 计算值的起始位置
				c_start = c_start + c_name.length + 1;
				// 查找值的结束位置
				c_end = document.cookie.indexOf(';', c_start);
				if (c_end == -1) c_end = document.cookie.length;
				// 返回解码后的Cookie值
				return unescape(document.cookie.substring(c_start, c_end));
			}
		}
		return null;
	}

	/**
	 * 为图表设置多语言支持
	 * @param {Object|string} langobj - 语言对象或字符串
	 * @param {Object} chartobj - 图表配置对象
	 * @returns {Object|string|boolean} 处理后的语言对象
	 */
	function setchartlang (langobj,chartobj) {
		// 如果是字符串，直接返回
		if ( $.type (langobj) == 'string' ) return langobj;
		// 如果是chartobj类型，返回false（这里可能是个bug，应该检查'object'）
		if ( $.type (langobj) == 'chartobj' ) return false;
		
		var flag = true;
		// 遍历语言对象的所有属性
		for (key in langobj) {
			var item = key;
			// 递归处理子对象
			children = (chartobj.hasOwnProperty(item)) ? setchartlang (langobj[item],chartobj[item]) : setchartlang (langobj[item],undefined);
			
			switch ($.type(children)) {
				case 'string':
					// 如果图表对象对应项不是字符串，跳过
					if ($.type(chartobj[item]) != 'string' ) continue;
				case 'object':
					// 设置图表对象的值
					chartobj[item] = (children['value'] || children);
				default:
					flag = false;
			}
		}
		// 如果处理成功，返回当前语言的值
		if (flag) { return {'value':(langobj[languages['current']] || langobj[languages['default']] || 'N/A')}}
	}

	/**
	 * jQuery插件：初始化多语言系统
	 * 从服务器加载语言配置文件，构建语言菜单，设置默认语言
	 */
	$.fn.cloudLang = function () {
		$.ajax({
			type: 'GET',
			url: window.nps.web_base_url + '/static/page/languages.xml',
			dataType: 'xml',
			success: function (xml) {
				// 解析XML语言配置文件
				languages['content'] = xml2json($(xml).children())['content'];
				languages['menu'] = languages['content']['languages'];
				languages['default'] = languages['content']['default'];
				
				// 获取用户语言偏好：Cookie > 浏览器语言
				languages['navigator'] = (getCookie ('lang') || navigator.language || navigator.browserLanguage);
				
				// 构建语言选择菜单
				for(var key in languages['menu']){
					$('#languagemenu').next().append('<li lang="' + key + '"><a><img src="' + window.nps.web_base_url + '/static/img/flag/' + key + '.png"> ' + languages['menu'][key] +'</a></li>');
					// 设置当前语言
					if ( key == languages['navigator'] ) languages['current'] = key;
				}
				
				// 设置语言菜单的当前语言属性
				$('#languagemenu').attr('lang',(languages['current'] || languages['default']));
				// 应用语言设置到整个页面
				$('body').setLang ('');
			}
		});
	};

	/**
	 * jQuery插件：应用语言设置到指定DOM元素
	 * @param {string} dom - DOM选择器，空字符串表示全局设置
	 */
	$.fn.setLang = function (dom) {
		// 获取当前选中的语言
		languages['current'] = $('#languagemenu').attr('lang');
		
		// 如果是全局设置（dom为空字符串）
		if ( dom == '' ) {
			// 更新语言菜单显示文本
			$('#languagemenu span').text(' ' + languages['menu'][languages['current']]);
			// 如果当前语言与Cookie中的不同，更新Cookie
			if (languages['current'] != getCookie('lang')) setCookie('lang', languages['current']);
			// 如果存在Bootstrap Table，更新其本地化设置
			if($("#table").length>0) $('#table').bootstrapTable('refreshOptions', { 'locale': languages['current']});
		}
		
		// 处理所有带有langtag属性的元素
		$.each($(dom + ' [langtag]'), function (i, item) {
			var index = $(item).attr('langtag');
			string = languages['content'][index.toLowerCase()];
			
			// 根据语言字符串类型进行不同处理
			switch ($.type(string)) {
				case 'string':
					// 字符串类型，直接使用
					break;
				case 'array':
					// 数组类型，随机选择一个
					string = string[Math.floor((Math.random()*string.length))];
				case 'object':
					// 对象类型，根据当前语言选择对应文本
					string = (string[languages['current']] || string[languages['default']] || null);
					break;
				default:
					// 未找到对应语言字符串，显示错误信息并高亮
					string = 'Missing language string "' + index + '"';
					$(item).css('background-color','#ffeeba');
			}
			
			// 根据元素类型设置文本或占位符
			if($.type($(item).attr('placeholder')) == 'undefined') {
				$(item).text(string);
			} else {
				$(item).attr('placeholder', string);
			}
		});

		// 处理图表的多语言设置
		if ( !$.isEmptyObject(chartdatas) ) {
			setchartlang(languages['content']['charts'],chartdatas);
			for(var key in chartdatas){
				// 检查对应的DOM元素是否存在
				if ($('#'+key).length == 0) continue;
				// 如果是对象类型，初始化ECharts图表
				if($.type(chartdatas[key]) == 'object')
				charts[key] = echarts.init(document.getElementById(key));
				// 应用图表配置
				charts[key].setOption(chartdatas[key], true);
			}
		}
	}

})(jQuery);

/**
 * 文档就绪事件处理
 * 初始化多语言系统并绑定语言切换事件
 */
$(document).ready(function () {
	// 初始化多语言系统
	$('body').cloudLang();
	
	// 绑定语言菜单点击事件
	$('body').on('click','li[lang]',function(){
		// 设置选中的语言
		$('#languagemenu').attr('lang',$(this).attr('lang'));
		// 应用新的语言设置
		$('body').setLang ('');
	});
});

// ========== 全局变量定义 ==========
var languages = {};    // 存储语言配置和状态
var charts = {};       // 存储ECharts图表实例
var chartdatas = {};   // 存储图表配置数据
var postsubmit;        // 表单提交状态标志

/**
 * 获取服务器响应消息的多语言版本
 * @param {string} langstr - 原始响应消息字符串
 * @returns {string} 翻译后的消息或原始消息
 */
function langreply(langstr) {
    // 清理字符串中的空格、逗号、句号、问号，转为小写后查找对应的语言对象
    var langobj = languages['content']['reply'][langstr.replace(/[\s,\.\?]*/g,"").toLowerCase()];
    // 如果没有找到对应的语言配置，返回原始字符串
    if ($.type(langobj) == 'undefined') return langstr
    // 根据当前语言、默认语言的优先级返回翻译文本
    langobj = (langobj[languages['current']] || langobj[languages['default']] || langstr);
    return langobj
}

/**
 * 通用表单提交函数，支持多语言确认对话框
 * @param {string} action - 操作类型：'start'|'stop'|'delete'|'add'|'edit'
 * @param {string} url - 提交的URL地址
 * @param {Array} postdata - 表单数据数组
 */
function submitform(action, url, postdata) {
    postsubmit = false;
    
    switch (action) {
        case 'start':
        case 'stop':
        case 'delete':
            // 危险操作需要用户确认
            var langobj = languages['content']['confirm'][action];
            // 获取确认消息的多语言版本
            action = (langobj[languages['current']] || langobj[languages['default']] || 'Are you sure you want to ' + action + ' it?');
            // 显示确认对话框，用户取消则直接返回
            if (! confirm(action)) return;
            postsubmit = true;
        case 'add':
        case 'edit':
            // 发送AJAX请求
            $.ajax({
                type: "POST",
                url: url,
                data: postdata,
                success: function (res) {
                    // 显示服务器响应消息（多语言）
                    alert(langreply(res.msg));
                    if (res.status) {
                        // 根据操作类型决定页面跳转方式
                        if (postsubmit) {
                            // 危险操作成功后刷新页面
                            document.location.reload();
                        } else {
                            // 添加/编辑操作成功后返回上一页
                            history.back(-1);
                        }
                    }
                }
            });
    }
}

/**
 * 将字节数转换为人类可读的存储单位格式
 * @param {number} limit - 字节数
 * @returns {string} 格式化后的存储大小字符串（B/KB/MB/GB）
 */
function changeunit(limit) {
    var size = "";
    
    // 根据大小选择合适的单位进行转换
    if (limit < 0.1 * 1024) {
        // 小于0.1KB，显示为字节
        size = limit.toFixed(2) + "B";
    } else if (limit < 0.1 * 1024 * 1024) {
        // 小于0.1MB，显示为KB
        size = (limit / 1024).toFixed(2) + "KB";
    } else if (limit < 0.1 * 1024 * 1024 * 1024) {
        // 小于0.1GB，显示为MB
        size = (limit / (1024 * 1024)).toFixed(2) + "MB";
    } else {
        // 大于等于0.1GB，显示为GB
        size = (limit / (1024 * 1024 * 1024)).toFixed(2) + "GB";
    }

    // 优化显示格式：如果小数部分为.00，则去掉小数部分
    var sizeStr = size + "";
    var index = sizeStr.indexOf(".");
    var dou = sizeStr.substr(index + 1, 2);
    if (dou == "00") {
        // 去掉.00，保留单位
        return sizeStr.substring(0, index) + sizeStr.substr(index + 3, 2);
    }
    return size;
}