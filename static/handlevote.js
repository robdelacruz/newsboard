function bindVoteListeners() {
    document.addEventListener("click", function(e) {
        console.log(e.target);
        let votebtn = null;
        if (e.target.classList.contains("upvote")) {
            votebtn = e.target;
        } else {
            votebtn = e.target.closest(".upvote");
        }
        if (votebtn == null) {
            return;
        }
        let wspath = "/vote/";
        if (votebtn.classList.contains("selfvote")) {
            wspath = "/unvote/";
        }

        let entry = votebtn.closest(".entry");
        if (entry == null) {
            return;
        }
        let votectr = entry.querySelector(".votectr");

        let tok = entry.getAttribute("data-votetok");
        if (tok == null || tok == "") {
            return;
        }

        let xhr = new XMLHttpRequest();
        xhr.onload = function() {
            if (xhr.status < 200 || xhr.status >= 300) {
                return;
            }
            let vr = JSON.parse(xhr.responseText);

            if (wspath == "/vote/") {
                votebtn.classList.add("selfvote");
            } else if (wspath == "/unvote/") {
                votebtn.classList.remove("selfvote");
            }
            if (votectr != null) {
                votectr.innerText = vr.totalvotes;
            }
        };
        console.log(`${wspath}?tok=${tok}`);
        xhr.open("GET", `${wspath}?tok=${tok}`);
        xhr.send();
    });
}

bindVoteListeners();
